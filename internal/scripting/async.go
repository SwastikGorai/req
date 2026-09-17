package scripting

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/dop251/goja"

	"req/internal/httpclient"
	"req/internal/model"
	"req/internal/variables"
)

const (
	maxAuxiliaryRequests   = 20
	maxAuxiliaryConcurrent = 4
	maxAuxiliaryBodyBytes  = 10 << 20
)

var auxiliaryHTTPToken = regexp.MustCompile(`^[!#$%&'*+\-.^_` + "`" + `|~0-9A-Za-z]+$`)

type auxRequest struct {
	method  string
	url     string
	headers [][2]string
	bodyDef *model.Body
	body    *httpclient.Body
}

type auxTask struct {
	request  auxRequest
	callback goja.Callable
}

// installSendRequest adds the callback form only. Promise settlement belongs
// to the next async phase; accepting a Promise here would report false
// success before that phase can track it.
func (e *engine) installSendRequest() {
	rt := e.rt
	mustSet(e.pmTarget, "sendRequest", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) != 2 {
			panic(rt.NewTypeError("pm.sendRequest(request, callback) requires exactly one callback"))
		}
		callback, ok := goja.AssertFunction(call.Argument(1))
		if !ok {
			panic(rt.NewTypeError("pm.sendRequest(request, callback) requires a callback function"))
		}
		request, err := e.parseAuxRequest(call.Argument(0))
		if err != nil {
			panic(rt.NewGoError(fmt.Errorf("pm.sendRequest: %w", err)))
		}
		e.scheduleAux(request, callback)
		return goja.Undefined()
	})
}

func (e *engine) scheduleAux(request auxRequest, callback goja.Callable) {
	if !e.running || e.runCtx == nil {
		panic(e.rt.NewGoError(fmt.Errorf("pm.sendRequest is only available during a script run")))
	}
	if e.auxTotal >= maxAuxiliaryRequests {
		panic(e.rt.NewGoError(fmt.Errorf("pm.sendRequest: maximum %d auxiliary requests per execution exceeded", maxAuxiliaryRequests)))
	}
	e.auxTotal++
	e.pending.Add(1)
	task := auxTask{request: request, callback: callback}
	if e.auxActive < maxAuxiliaryConcurrent {
		e.launchAux(task)
		return
	}
	e.auxQueue = append(e.auxQueue, task)
}

func (e *engine) launchAux(task auxTask) {
	e.auxActive++
	ctx, generation := e.runCtx, e.runGen
	e.workers.Add(1)
	go func() {
		defer e.workers.Done()
		result := e.fetchAux(ctx, task.request)
		e.enqueue(func() {
			if !e.running || generation != e.runGen {
				return
			}
			e.auxActive--
			e.deliverAux(task, result)
		})
	}()
}

func (e *engine) startQueuedAux() {
	for e.runErr == nil && !e.skipRequested && e.runCtx != nil && e.runCtx.Err() == nil &&
		e.auxActive < maxAuxiliaryConcurrent && len(e.auxQueue) > 0 {
		task := e.auxQueue[0]
		e.auxQueue = e.auxQueue[1:]
		e.launchAux(task)
	}
}

// deliverAux is called on the owner goroutine. The callback is the only
// place where the response becomes a JS value.
func (e *engine) deliverAux(task auxTask, result httpResult) {
	defer e.pending.Add(-1)
	var callbackErr error
	if result.err != nil {
		_, callbackErr = task.callback(goja.Undefined(), result.errValue(e.rt), goja.Undefined())
	} else {
		_, callbackErr = task.callback(goja.Undefined(), goja.Null(), e.auxResponseValue(result))
	}
	if callbackErr != nil {
		e.setRunErr(fmt.Errorf("pm.sendRequest callback: %w", callbackErr))
	}
	if callbackErr == nil {
		e.startQueuedAux()
	}
}

func (e *engine) auxResponseValue(result httpResult) goja.Value {
	if result.response == nil {
		return goja.Undefined()
	}
	return e.buildResponse(result.response)
}

func (e *engine) fetchAux(ctx context.Context, request auxRequest) httpResult {
	var body io.ReadCloser
	var err error
	if request.body != nil {
		body, err = request.body.Open(ctx)
		if err != nil {
			return httpResult{err: fmt.Errorf("opening auxiliary request body: %w", err)}
		}
		defer body.Close()
	}
	headers := http.Header{}
	for _, header := range request.headers {
		headers.Add(header[0], header[1])
	}
	response, err := httpclient.Send(ctx, e.httpClient, request.method, request.url, body, headers)
	if err != nil {
		return httpResult{err: err}
	}
	if response.Body == nil {
		return httpResult{response: &ResponseData{
			Code: response.StatusCode, Status: http.StatusText(response.StatusCode),
			TimeMS: response.Duration.Milliseconds(), Headers: response.Headers,
		}}
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, maxAuxiliaryBodyBytes+1))
	if err != nil {
		return httpResult{status: response.StatusCode, err: fmt.Errorf("reading auxiliary response body: %w", err)}
	}
	if len(data) > maxAuxiliaryBodyBytes {
		return httpResult{status: response.StatusCode,
			err: fmt.Errorf("auxiliary response body exceeds the 10 MiB limit")}
	}
	return httpResult{
		status: response.StatusCode,
		body:   string(data),
		response: &ResponseData{
			Code: response.StatusCode, Status: http.StatusText(response.StatusCode),
			TimeMS: response.Duration.Milliseconds(), Headers: response.Headers, Body: data,
		},
	}
}

func (e *engine) parseAuxRequest(value goja.Value) (auxRequest, error) {
	if exported, ok := value.Export().(string); ok {
		request := auxRequest{method: http.MethodGet, url: exported}
		if err := e.resolveAuxRequest(&request, nil); err != nil {
			return auxRequest{}, err
		}
		return request, nil
	}
	object, err := requireObject(value, "request")
	if err != nil {
		return auxRequest{}, err
	}
	if err := rejectUnknown(object, map[string]bool{
		"auth": true, "body": true, "header": true, "method": true, "url": true,
	}, "request"); err != nil {
		return auxRequest{}, err
	}
	urlValue := object.Get("url")
	if urlValue == nil || goja.IsUndefined(urlValue) || goja.IsNull(urlValue) {
		return auxRequest{}, fmt.Errorf("request.url is required")
	}
	rawURL, err := e.parseAuxURL(urlValue)
	if err != nil {
		return auxRequest{}, err
	}
	method, found, err := optionalString(object, "method", "request.method")
	if err != nil {
		return auxRequest{}, err
	}
	if !found {
		method = http.MethodGet
	}
	headers, err := e.parseAuxHeaders(object.Get("header"))
	if err != nil {
		return auxRequest{}, err
	}
	body, err := e.parseAuxBody(object.Get("body"))
	if err != nil {
		return auxRequest{}, err
	}
	auth, err := e.parseAuxAuth(object.Get("auth"))
	if err != nil {
		return auxRequest{}, err
	}
	request := auxRequest{method: method, url: rawURL, headers: headers, bodyDef: body}
	if err := e.resolveAuxRequest(&request, auth); err != nil {
		return auxRequest{}, err
	}
	return request, nil
}

func (e *engine) parseAuxURL(value goja.Value) (string, error) {
	if raw, ok := value.Export().(string); ok {
		return e.scope.ResolveString(raw, "pm.sendRequest.url")
	}
	object, err := requireObject(value, "request.url")
	if err != nil {
		return "", err
	}
	if err := rejectUnknown(object, map[string]bool{
		"host": true, "path": true, "protocol": true, "query": true, "raw": true,
	}, "request.url"); err != nil {
		return "", err
	}
	raw, found, err := optionalString(object, "raw", "request.url.raw")
	if err != nil {
		return "", err
	}
	query := object.Get("query")
	if found && raw != "" {
		raw, err = e.scope.ResolveString(raw, "pm.sendRequest.url")
		if err != nil {
			return "", err
		}
		parsed, parseErr := url.Parse(raw)
		if parseErr != nil {
			return "", parseErr
		}
		if query != nil && !goja.IsUndefined(query) && !goja.IsNull(query) && parsed.RawQuery == "" {
			return e.appendAuxURLQuery(raw, query)
		}
		return raw, nil
	}
	protocol, protocolFound, err := optionalString(object, "protocol", "request.url.protocol")
	if err != nil {
		return "", err
	}
	host, hostFound, err := e.parseURLPart(object.Get("host"), "request.url.host", ".")
	if err != nil {
		return "", err
	}
	path, _, err := e.parseURLPart(object.Get("path"), "request.url.path", "/")
	if err != nil {
		return "", err
	}
	if !protocolFound || protocol == "" || !hostFound || host == "" {
		return "", fmt.Errorf("request.url needs raw or protocol and host")
	}
	protocol, err = e.scope.ResolveString(protocol, "pm.sendRequest.url.protocol")
	if err != nil {
		return "", err
	}
	host, err = e.scope.ResolveString(host, "pm.sendRequest.url.host")
	if err != nil {
		return "", err
	}
	path, err = e.scope.ResolveString(path, "pm.sendRequest.url.path")
	if err != nil {
		return "", err
	}
	raw = protocol + "://" + host
	if path != "" {
		if !strings.HasPrefix(path, "/") {
			raw += "/"
		}
		raw += path
	}
	if query == nil || goja.IsUndefined(query) || goja.IsNull(query) {
		return raw, nil
	}
	return e.appendAuxURLQuery(raw, query)
}

func (e *engine) parseURLPart(value goja.Value, where, separator string) (string, bool, error) {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return "", false, nil
	}
	if text, ok := value.Export().(string); ok {
		return text, true, nil
	}
	object, err := requireObject(value, where)
	if err != nil || object.ClassName() != "Array" {
		if err != nil {
			return "", false, err
		}
		return "", false, fmt.Errorf("%s must be a string or array", where)
	}
	items, err := arrayItems(object, where)
	if err != nil {
		return "", false, err
	}
	parts := make([]string, len(items))
	for i, item := range items {
		text, ok := item.Export().(string)
		if !ok {
			return "", false, fmt.Errorf("%s[%d] must be a string", where, i)
		}
		parts[i] = text
	}
	return strings.Join(parts, separator), true, nil
}

func (e *engine) appendAuxURLQuery(raw string, value goja.Value) (string, error) {
	object, err := requireObject(value, "request.url.query")
	if err != nil || object.ClassName() != "Array" {
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("request.url.query must be an array")
	}
	items, err := arrayItems(object, "request.url.query")
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	for i, item := range items {
		entry, enabled, err := parseEnabledEntry(item, fmt.Sprintf("request.url.query[%d]", i))
		if err != nil {
			return "", err
		}
		if !enabled {
			continue
		}
		key, err := e.scope.ResolveString(entry[0], fmt.Sprintf("pm.sendRequest.url.query[%d].key", i))
		if err != nil {
			return "", err
		}
		if key == "" {
			return "", fmt.Errorf("empty auxiliary URL query key at index %d", i)
		}
		value, err := e.scope.ResolveString(entry[1], fmt.Sprintf("pm.sendRequest.url.query[%d].value", i))
		if err != nil {
			return "", err
		}
		separator := "&"
		if parsed.RawQuery == "" {
			separator = ""
		}
		parsed.RawQuery += separator + url.QueryEscape(key) + "=" + url.QueryEscape(value)
	}
	return parsed.String(), nil
}

func (e *engine) resolveAuxRequest(request *auxRequest, auth *model.Auth) error {
	resolved, err := e.scope.ResolveString(request.method, "pm.sendRequest.method")
	if err != nil {
		return err
	}
	request.method = strings.ToUpper(resolved)
	if !auxiliaryHTTPToken.MatchString(request.method) {
		return fmt.Errorf("invalid auxiliary HTTP method %q", request.method)
	}
	request.url, err = e.scope.ResolveString(request.url, "pm.sendRequest.url")
	if err != nil {
		return err
	}
	if parsed, parseErr := url.Parse(request.url); parseErr != nil || parsed.Host == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("URL must be absolute http or https, got %q", request.url)
	}
	for i := range request.headers {
		request.headers[i][0], err = e.scope.ResolveString(request.headers[i][0], fmt.Sprintf("pm.sendRequest.header[%d].name", i))
		if err != nil {
			return err
		}
		request.headers[i][1], err = e.scope.ResolveString(request.headers[i][1], fmt.Sprintf("pm.sendRequest.header[%d].value", i))
		if err != nil {
			return err
		}
		if !auxiliaryHTTPToken.MatchString(request.headers[i][0]) || strings.ContainsAny(request.headers[i][1], "\r\n") {
			return fmt.Errorf("invalid auxiliary header at index %d", i)
		}
	}
	if err := resolveAuxBody(e.scope, request.bodyDef); err != nil {
		return err
	}
	if auth != nil {
		if err := resolveAuxAuth(e.scope, auth); err != nil {
			return err
		}
		if !hasAuxHeader(request.headers, "Authorization") {
			value, err := auxAuthorization(*auth)
			if err != nil {
				return err
			}
			if value != "" {
				request.headers = append(request.headers, [2]string{"Authorization", value})
			}
		}
	}
	contentType := ""
	for _, header := range request.headers {
		if strings.EqualFold(header[0], "Content-Type") {
			contentType = header[1]
			break
		}
	}
	request.body, err = httpclient.BuildBody(request.bodyDef, contentType)
	if err != nil {
		return err
	}
	if request.body != nil && !hasAuxHeader(request.headers, "Content-Type") {
		request.headers = append(request.headers, [2]string{"Content-Type", request.body.ContentType})
	}
	return nil
}

func (e *engine) parseAuxHeaders(value goja.Value) ([][2]string, error) {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return nil, nil
	}
	object, err := requireObject(value, "request.header")
	if err != nil {
		return nil, err
	}
	if object.ClassName() == "Array" {
		items, err := arrayItems(object, "request.header")
		if err != nil {
			return nil, err
		}
		out := make([][2]string, 0, len(items))
		for i, item := range items {
			entry, enabled, err := parseEnabledEntry(item, fmt.Sprintf("request.header[%d]", i))
			if err != nil {
				return nil, err
			}
			if enabled {
				out = append(out, entry)
			}
		}
		return out, nil
	}
	keys := object.Keys()
	sort.Strings(keys)
	out := make([][2]string, 0, len(keys))
	for _, key := range keys {
		value, ok := object.Get(key).Export().(string)
		if !ok {
			return nil, fmt.Errorf("request.header[%q] must be a string", key)
		}
		out = append(out, [2]string{key, value})
	}
	return out, nil
}

func (e *engine) parseAuxBody(value goja.Value) (*model.Body, error) {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return nil, nil
	}
	object, err := requireObject(value, "request.body")
	if err != nil {
		return nil, err
	}
	mode, found, err := optionalString(object, "mode", "request.body.mode")
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("request.body.mode is required")
	}
	switch mode {
	case "raw":
		if err := rejectUnknown(object, map[string]bool{"mode": true, "raw": true}, "request.body"); err != nil {
			return nil, err
		}
		raw, found, err := optionalString(object, "raw", "request.body.raw")
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("request.body.raw is required")
		}
		return &model.Body{Type: "raw", Text: &raw}, nil
	case "urlencoded":
		if err := rejectUnknown(object, map[string]bool{"mode": true, "urlencoded": true}, "request.body"); err != nil {
			return nil, err
		}
		entries, err := e.parseAuxBodyEntries(object.Get("urlencoded"), "request.body.urlencoded")
		if err != nil {
			return nil, err
		}
		return &model.Body{Type: "urlencoded", URLEncoded: entries}, nil
	case "formdata":
		if err := rejectUnknown(object, map[string]bool{"mode": true, "formdata": true}, "request.body"); err != nil {
			return nil, err
		}
		fields, err := e.parseAuxFormData(object.Get("formdata"))
		if err != nil {
			return nil, err
		}
		return &model.Body{Type: "multipart", Multipart: fields}, nil
	default:
		return nil, fmt.Errorf("unsupported auxiliary body mode %q", mode)
	}
}

func (e *engine) parseAuxBodyEntries(value goja.Value, where string) ([]model.Entry, error) {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return nil, fmt.Errorf("%s must be an array", where)
	}
	object, err := requireObject(value, where)
	if err != nil || object.ClassName() != "Array" {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%s must be an array", where)
	}
	items, err := arrayItems(object, where)
	if err != nil {
		return nil, err
	}
	entries := make([]model.Entry, 0, len(items))
	for i, item := range items {
		entry, enabled, err := parseEnabledEntry(item, fmt.Sprintf("%s[%d]", where, i))
		if err != nil {
			return nil, err
		}
		entries = append(entries, model.Entry{Key: entry[0], Value: entry[1], Enabled: enabled})
	}
	return entries, nil
}

func (e *engine) parseAuxFormData(value goja.Value) ([]model.MultipartField, error) {
	where := "request.body.formdata"
	object, err := requireObject(value, where)
	if err != nil || object.ClassName() != "Array" {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("%s must be an array", where)
	}
	items, err := arrayItems(object, where)
	if err != nil {
		return nil, err
	}
	fields := make([]model.MultipartField, 0, len(items))
	for i, item := range items {
		field, err := requireObject(item, fmt.Sprintf("%s[%d]", where, i))
		if err != nil {
			return nil, err
		}
		if err := rejectUnknown(field, map[string]bool{
			"contentType": true, "disabled": true, "key": true, "type": true, "value": true,
		}, fmt.Sprintf("%s[%d]", where, i)); err != nil {
			return nil, err
		}
		key, found, err := optionalString(field, "key", fmt.Sprintf("%s[%d].key", where, i))
		if err != nil || !found {
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("%s[%d].key is required", where, i)
		}
		typ, found, err := optionalString(field, "type", fmt.Sprintf("%s[%d].type", where, i))
		if err != nil {
			return nil, err
		}
		if found && typ != "text" {
			return nil, fmt.Errorf("%s[%d]: file formdata fields are not supported", where, i)
		}
		value, found, err := optionalString(field, "value", fmt.Sprintf("%s[%d].value", where, i))
		if err != nil {
			return nil, err
		}
		contentType, foundContentType, err := optionalString(field, "contentType", fmt.Sprintf("%s[%d].contentType", where, i))
		if err != nil {
			return nil, err
		}
		disabled, _, err := optionalBool(field, "disabled", fmt.Sprintf("%s[%d].disabled", where, i))
		if err != nil {
			return nil, err
		}
		fields = append(fields, model.MultipartField{
			Key: key, Value: optionalStringPtr(value, found), Enabled: !disabled,
			ContentType: valueIf(foundContentType, contentType),
		})
	}
	return fields, nil
}

func (e *engine) parseAuxAuth(value goja.Value) (*model.Auth, error) {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return nil, nil
	}
	object, err := requireObject(value, "request.auth")
	if err != nil {
		return nil, err
	}
	if err := rejectUnknown(object, map[string]bool{
		"basic": true, "bearer": true, "password": true, "token": true, "type": true, "username": true,
	}, "request.auth"); err != nil {
		return nil, err
	}
	typ, found, err := optionalString(object, "type", "request.auth.type")
	if err != nil || !found {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("request.auth.type is required")
	}
	switch typ {
	case "none", "noauth":
		return &model.Auth{Type: "none"}, nil
	case "bearer":
		token, found, err := optionalString(object, "token", "request.auth.token")
		if err != nil {
			return nil, err
		}
		if !found {
			token, found, err = nestedAuthField(object.Get("bearer"), "token", "request.auth.bearer")
			if err != nil {
				return nil, err
			}
		}
		if !found {
			return nil, fmt.Errorf("request.auth bearer token is required")
		}
		return &model.Auth{Type: "bearer", Token: token}, nil
	case "basic":
		username, userFound, err := optionalString(object, "username", "request.auth.username")
		if err != nil {
			return nil, err
		}
		password, passFound, err := optionalString(object, "password", "request.auth.password")
		if err != nil {
			return nil, err
		}
		if !userFound {
			username, userFound, err = nestedAuthField(object.Get("basic"), "username", "request.auth.basic")
			if err != nil {
				return nil, err
			}
		}
		if !passFound {
			password, passFound, err = nestedAuthField(object.Get("basic"), "password", "request.auth.basic")
			if err != nil {
				return nil, err
			}
		}
		if !userFound || !passFound {
			return nil, fmt.Errorf("request.auth basic username and password are required")
		}
		return &model.Auth{Type: "basic", Username: username, Password: password}, nil
	default:
		return nil, fmt.Errorf("unsupported auxiliary auth type %q", typ)
	}
}

func resolveAuxBody(scope *variables.Scope, body *model.Body) error {
	if body == nil {
		return nil
	}
	var err error
	if body.Text != nil {
		value, resolveErr := scope.ResolveString(*body.Text, "pm.sendRequest.body")
		if resolveErr != nil {
			return resolveErr
		}
		body.Text = &value
	}
	for i := range body.URLEncoded {
		body.URLEncoded[i].Key, err = scope.ResolveString(body.URLEncoded[i].Key, fmt.Sprintf("pm.sendRequest.body.urlencoded[%d].key", i))
		if err != nil {
			return err
		}
		body.URLEncoded[i].Value, err = scope.ResolveString(body.URLEncoded[i].Value, fmt.Sprintf("pm.sendRequest.body.urlencoded[%d].value", i))
		if err != nil {
			return err
		}
	}
	for i := range body.Multipart {
		body.Multipart[i].Key, err = scope.ResolveString(body.Multipart[i].Key, fmt.Sprintf("pm.sendRequest.body.formdata[%d].key", i))
		if err != nil {
			return err
		}
		if body.Multipart[i].Value != nil {
			value, resolveErr := scope.ResolveString(*body.Multipart[i].Value, fmt.Sprintf("pm.sendRequest.body.formdata[%d].value", i))
			if resolveErr != nil {
				return resolveErr
			}
			body.Multipart[i].Value = &value
		}
	}
	return nil
}

func resolveAuxAuth(scope *variables.Scope, auth *model.Auth) error {
	var err error
	switch auth.Type {
	case "bearer":
		auth.Token, err = scope.ResolveString(auth.Token, "pm.sendRequest.auth token")
	case "basic":
		auth.Username, err = scope.ResolveString(auth.Username, "pm.sendRequest.auth username")
		if err == nil {
			auth.Password, err = scope.ResolveString(auth.Password, "pm.sendRequest.auth password")
		}
	}
	return err
}

func auxAuthorization(auth model.Auth) (string, error) {
	switch auth.Type {
	case "none", "inherit":
		return "", nil
	case "bearer":
		if strings.ContainsAny(auth.Token, "\r\n") {
			return "", fmt.Errorf("invalid auxiliary auth header")
		}
		return "Bearer " + auth.Token, nil
	case "basic":
		if strings.Contains(auth.Username, ":") {
			return "", fmt.Errorf("basic username cannot contain a colon")
		}
		value := base64.StdEncoding.EncodeToString([]byte(auth.Username + ":" + auth.Password))
		return "Basic " + value, nil
	default:
		return "", fmt.Errorf("unsupported auxiliary auth type %q", auth.Type)
	}
}

func hasAuxHeader(headers [][2]string, name string) bool {
	for _, header := range headers {
		if strings.EqualFold(header[0], name) {
			return true
		}
	}
	return false
}

func parseEnabledEntry(value goja.Value, where string) ([2]string, bool, error) {
	object, err := requireObject(value, where)
	if err != nil {
		return [2]string{}, false, err
	}
	if err := rejectUnknown(object, map[string]bool{"disabled": true, "enabled": true, "key": true, "value": true}, where); err != nil {
		return [2]string{}, false, err
	}
	key, found, err := optionalString(object, "key", where+".key")
	if err != nil || !found {
		if err != nil {
			return [2]string{}, false, err
		}
		return [2]string{}, false, fmt.Errorf("%s.key is required", where)
	}
	valueText, found, err := optionalString(object, "value", where+".value")
	if err != nil {
		return [2]string{}, false, err
	}
	if !found {
		valueText = ""
	}
	disabled, disabledFound, err := optionalBool(object, "disabled", where+".disabled")
	if err != nil {
		return [2]string{}, false, err
	}
	enabled, enabledFound, err := optionalBool(object, "enabled", where+".enabled")
	if err != nil {
		return [2]string{}, false, err
	}
	if disabledFound && disabled {
		return [2]string{key, valueText}, false, nil
	}
	if enabledFound && !enabled {
		return [2]string{key, valueText}, false, nil
	}
	return [2]string{key, valueText}, true, nil
}

func nestedAuthField(value goja.Value, name, where string) (string, bool, error) {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return "", false, nil
	}
	object, err := requireObject(value, where)
	if err != nil {
		return "", false, err
	}
	if object.ClassName() == "Array" {
		items, err := arrayItems(object, where)
		if err != nil {
			return "", false, err
		}
		for i, item := range items {
			entry, err := requireObject(item, fmt.Sprintf("%s[%d]", where, i))
			if err != nil {
				return "", false, err
			}
			key, keyFound, err := optionalString(entry, "key", where+".key")
			if err != nil {
				return "", false, err
			}
			if !keyFound || key != name {
				continue
			}
			disabled, _, err := optionalBool(entry, "disabled", where+".disabled")
			if err != nil {
				return "", false, err
			}
			if disabled {
				return "", false, nil
			}
			return optionalString(entry, "value", where+".value")
		}
		return "", false, nil
	}
	return optionalString(object, name, where+"."+name)
}

func requireObject(value goja.Value, where string) (*goja.Object, error) {
	if goja.IsUndefined(value) || goja.IsNull(value) {
		return nil, fmt.Errorf("%s must be an object", where)
	}
	object, ok := value.(*goja.Object)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", where)
	}
	return object, nil
}

func rejectUnknown(object *goja.Object, allowed map[string]bool, where string) error {
	for _, key := range object.Keys() {
		if !allowed[key] {
			return fmt.Errorf("unsupported %s property %q", where, key)
		}
	}
	return nil
}

func arrayItems(object *goja.Object, where string) ([]goja.Value, error) {
	if object.ClassName() != "Array" {
		return nil, fmt.Errorf("%s must be an array", where)
	}
	length := object.Get("length").ToInteger()
	if length < 0 {
		return nil, fmt.Errorf("%s has an invalid length", where)
	}
	items := make([]goja.Value, length)
	for i := range items {
		items[i] = object.Get(fmt.Sprint(i))
	}
	return items, nil
}

func optionalString(object *goja.Object, name, where string) (string, bool, error) {
	value := object.Get(name)
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return "", false, nil
	}
	text, ok := value.Export().(string)
	if !ok {
		return "", false, fmt.Errorf("%s must be a string", where)
	}
	return text, true, nil
}

func optionalBool(object *goja.Object, name, where string) (bool, bool, error) {
	value := object.Get(name)
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return false, false, nil
	}
	boolean, ok := value.Export().(bool)
	if !ok {
		return false, false, fmt.Errorf("%s must be a boolean", where)
	}
	return boolean, true, nil
}

func optionalStringPtr(value string, found bool) *string {
	if !found {
		return nil
	}
	return &value
}

func valueIf(found bool, value string) string {
	if found {
		return value
	}
	return ""
}
