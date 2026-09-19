package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"req/internal/model"
	"req/internal/store"
)

// CurlResult is one normalized cURL request and any non-fatal conversion
// warnings. Parsing never invokes a shell or reads attachment files.
type CurlResult struct {
	Request  model.Request
	Warnings []Warning
}

// ParseCurl parses one POSIX-like curl invocation into the native request
// shape. Options.Name is ignored here; Options.Source is retained as
// provenance.
func ParseCurl(data []byte, opts Options) (CurlResult, error) {
	tokens, err := tokenizeCurl(string(data))
	if err != nil {
		return CurlResult{}, fmt.Errorf("curl: %w", err)
	}
	p := curlParser{}
	request, err := p.parse(tokens)
	if err != nil {
		return CurlResult{}, fmt.Errorf("curl: %w", err)
	}
	original, _ := json.Marshal(string(data))
	request.Import = &model.ImportMetadata{
		Source:   "curl",
		Path:     opts.Source,
		Original: original,
	}
	for _, warning := range p.warnings {
		request.Import.Warnings = append(request.Import.Warnings, warning.Error())
	}
	result := CurlResult{Request: request, Warnings: append([]Warning(nil), p.warnings...)}
	if opts.Strict && len(result.Warnings) > 0 {
		return result, &StrictError{Warnings: result.Warnings}
	}
	return result, nil
}

// ImportCurl parses one cURL invocation and stores it at Options.Name. The
// destination is an existing collection/folder path; attachment references
// remain marked untrusted until a user remaps them.
func ImportCurl(ctx context.Context, ws *store.Workspace, data []byte, opts Options) (CurlResult, error) {
	result, err := ParseCurl(data, opts)
	if err != nil {
		return result, err
	}
	if opts.Name == "" {
		return result, fmt.Errorf("curl: --save-as requires a destination path")
	}
	if err := ws.CreateRequest(ctx, opts.Name, result.Request, false); err != nil {
		return result, err
	}
	return result, nil
}

type curlData struct {
	value string
	file  string
}

type curlParser struct {
	method   string
	urls     []string
	headers  [][2]string
	data     []curlData
	forms    []model.MultipartField
	mode     string
	auth     *model.Auth
	get      bool
	follow   bool
	insecure bool
	warnings []Warning
}

func (p *curlParser) parse(tokens []string) (model.Request, error) {
	if len(tokens) == 0 || tokens[0] != "curl" {
		return model.Request{}, fmt.Errorf("command must start with curl")
	}
	endOptions := false
	for i := 1; i < len(tokens); i++ {
		arg := tokens[i]
		if !endOptions && arg == "--" {
			endOptions = true
			continue
		}
		if !endOptions && strings.HasPrefix(arg, "--") {
			if err := p.longOption(tokens, &i, arg); err != nil {
				return model.Request{}, err
			}
			continue
		}
		if !endOptions && strings.HasPrefix(arg, "-") && arg != "-" {
			if err := p.shortOption(tokens, &i, arg); err != nil {
				return model.Request{}, err
			}
			continue
		}
		p.urls = append(p.urls, arg)
	}
	if len(p.urls) != 1 {
		return model.Request{}, fmt.Errorf("exactly one URL is required (got %d)", len(p.urls))
	}
	rawURL := p.urls[0]
	if strings.HasPrefix(rawURL, "@") {
		return model.Request{}, fmt.Errorf("URL file references are unsupported; provide one URL")
	}
	if err := validateCurlURL(rawURL); err != nil {
		return model.Request{}, err
	}
	if p.get && p.mode == "form" {
		return model.Request{}, fmt.Errorf("--get cannot be combined with --form")
	}
	if p.get && p.mode == "json" {
		return model.Request{}, fmt.Errorf("--get cannot be combined with --json")
	}

	method := p.method
	if method == "" {
		method = "GET"
		if p.mode != "" && !p.get {
			method = "POST"
		}
	}
	method = strings.ToUpper(method)
	if !validCurlToken(method) {
		return model.Request{}, fmt.Errorf("invalid request method %q", method)
	}

	request := model.Request{
		Method:          method,
		URL:             rawURL,
		Headers:         curlEntries(p.headers),
		Auth:            p.auth,
		FollowRedirects: boolPointer(p.follow),
		InsecureTLS:     p.insecure,
	}
	if p.get && len(p.data) > 0 {
		query, err := p.joinData(true)
		if err != nil {
			return model.Request{}, err
		}
		request.URL, err = appendCurlQuery(rawURL, query)
		if err != nil {
			return model.Request{}, err
		}
	} else {
		body, err := p.body()
		if err != nil {
			return model.Request{}, err
		}
		request.Body = body
	}
	if p.mode == "data" && !p.get {
		request.Headers = addCurlHeader(request.Headers, "Content-Type", "application/x-www-form-urlencoded")
	}
	if p.mode == "json" {
		request.Headers = addCurlHeader(request.Headers, "Content-Type", "application/json")
		request.Headers = addCurlHeader(request.Headers, "Accept", "application/json")
	}
	if request.Body != nil {
		if err := request.Body.Validate(); err != nil {
			return model.Request{}, err
		}
	}
	return request, nil
}

func (p *curlParser) longOption(tokens []string, i *int, arg string) error {
	name, inline, hasInline := strings.Cut(arg, "=")
	value := func(label string) (string, error) {
		if hasInline {
			return inline, nil
		}
		if *i+1 >= len(tokens) {
			return "", fmt.Errorf("%s requires a value", label)
		}
		*i++
		return tokens[*i], nil
	}
	boolean := func(label string, target *bool) error {
		if hasInline {
			return fmt.Errorf("%s does not take a value", label)
		}
		*target = true
		return nil
	}
	switch name {
	case "--request":
		v, err := value(name)
		if err != nil {
			return err
		}
		p.method = v
	case "--header":
		v, err := value(name)
		if err != nil {
			return err
		}
		return p.addHeader(v)
	case "--data", "--data-raw", "--data-binary", "--json":
		v, err := value(name)
		if err != nil {
			return err
		}
		return p.addData(name, v)
	case "--url":
		v, err := value(name)
		if err != nil {
			return err
		}
		p.urls = append(p.urls, v)
	case "--user":
		v, err := value(name)
		if err != nil {
			return err
		}
		return p.setUser(v)
	case "--form":
		v, err := value(name)
		if err != nil {
			return err
		}
		return p.addForm(v)
	case "--form-string":
		v, err := value(name)
		if err != nil {
			return err
		}
		return p.addFormString(v)
	case "--get":
		return boolean(name, &p.get)
	case "--location":
		return boolean(name, &p.follow)
	case "--insecure":
		return boolean(name, &p.insecure)
	default:
		return fmt.Errorf("unknown flag %q", name)
	}
	return nil
}

func (p *curlParser) shortOption(tokens []string, i *int, arg string) error {
	letters := arg[1:]
	for len(letters) > 0 {
		letter := letters[0]
		switch letter {
		case 'G':
			p.get = true
			letters = letters[1:]
		case 'L':
			p.follow = true
			letters = letters[1:]
		case 'k':
			p.insecure = true
			letters = letters[1:]
		case 'X', 'H', 'd', 'u', 'F':
			value := letters[1:]
			if value == "" {
				if *i+1 >= len(tokens) {
					return fmt.Errorf("-%c requires a value", letter)
				}
				*i++
				value = tokens[*i]
			}
			switch letter {
			case 'X':
				p.method = value
			case 'H':
				if err := p.addHeader(value); err != nil {
					return err
				}
			case 'd':
				if err := p.addData("--data", value); err != nil {
					return err
				}
			case 'u':
				if err := p.setUser(value); err != nil {
					return err
				}
			case 'F':
				if err := p.addForm(value); err != nil {
					return err
				}
			}
			return nil
		default:
			return fmt.Errorf("unknown flag %q", "-"+string(letter))
		}
	}
	return nil
}

func (p *curlParser) addHeader(value string) error {
	if strings.HasPrefix(value, "@") {
		return fmt.Errorf("header file references are unsupported")
	}
	name, headerValue, ok := strings.Cut(value, ":")
	name, headerValue = strings.TrimSpace(name), strings.TrimSpace(headerValue)
	if !ok || name == "" || !validCurlToken(name) {
		return fmt.Errorf("invalid header %q (want Name: value)", value)
	}
	p.headers = append(p.headers, [2]string{name, headerValue})
	return nil
}

func (p *curlParser) addData(flag, value string) error {
	mode := "data"
	if flag == "--json" {
		mode = "json"
	}
	if err := p.setMode(mode); err != nil {
		return err
	}
	d := curlData{value: value}
	if flag != "--data-raw" && strings.HasPrefix(value, "@") {
		if value == "@" || value == "@-" {
			return fmt.Errorf("stdin data references are unsupported")
		}
		d.file = strings.TrimPrefix(value, "@")
		d.value = ""
		if flag == "--data" {
			p.warn("data", "--data file references are preserved without curl's file newline/NUL stripping")
		}
	}
	p.data = append(p.data, d)
	return nil
}

func (p *curlParser) addForm(value string) error {
	if err := p.setMode("form"); err != nil {
		return err
	}
	name, formValue, hasValue := strings.Cut(value, "=")
	if name == "" {
		return fmt.Errorf("invalid form entry %q", value)
	}
	field := model.MultipartField{Key: name, Enabled: true}
	if !hasValue {
		formValue = ""
	}
	parts := strings.Split(formValue, ";")
	first := parts[0]
	if strings.HasPrefix(first, "@") {
		path := strings.TrimPrefix(first, "@")
		if path == "" || path == "-" {
			return fmt.Errorf("stdin form references are unsupported")
		}
		field.File, field.FileUntrusted = path, true
	} else if strings.HasPrefix(first, "<") {
		return fmt.Errorf("form file-content references using < are unsupported")
	} else {
		field.Value = &first
	}
	for _, option := range parts[1:] {
		if option == "" {
			continue
		}
		key, optionValue, ok := strings.Cut(option, "=")
		if !ok {
			return fmt.Errorf("unsupported form attribute %q", option)
		}
		switch strings.ToLower(key) {
		case "type":
			field.ContentType = optionValue
		case "filename":
			field.Filename = optionValue
		default:
			return fmt.Errorf("unsupported form attribute %q", key)
		}
	}
	p.forms = append(p.forms, field)
	return nil
}

func (p *curlParser) addFormString(value string) error {
	if err := p.setMode("form"); err != nil {
		return err
	}
	name, formValue, ok := strings.Cut(value, "=")
	if !ok || name == "" {
		return fmt.Errorf("invalid form-string entry %q", value)
	}
	formValueCopy := formValue
	p.forms = append(p.forms, model.MultipartField{Key: name, Value: &formValueCopy, Enabled: true})
	return nil
}

func (p *curlParser) setUser(value string) error {
	if strings.HasPrefix(value, "@") {
		return fmt.Errorf("netrc user references are unsupported")
	}
	user, password, _ := strings.Cut(value, ":")
	if user == "" {
		return fmt.Errorf("--user requires a non-empty username")
	}
	p.auth = &model.Auth{Type: "basic", Username: user, Password: password}
	return nil
}

func (p *curlParser) setMode(mode string) error {
	if p.mode != "" && p.mode != mode {
		return fmt.Errorf("body flags %q and %q cannot be combined", p.mode, mode)
	}
	p.mode = mode
	return nil
}

func (p *curlParser) body() (*model.Body, error) {
	switch p.mode {
	case "":
		return nil, nil
	case "data":
		joined, err := p.joinData(false)
		if err != nil {
			return nil, err
		}
		if len(p.data) == 1 && p.data[0].file != "" {
			return &model.Body{Type: "raw", File: p.data[0].file, FileUntrusted: true}, nil
		}
		return &model.Body{Type: "raw", Text: &joined}, nil
	case "json":
		joined, err := p.joinData(false)
		if err != nil {
			return nil, err
		}
		if len(p.data) == 1 && p.data[0].file != "" {
			return &model.Body{Type: "json", File: p.data[0].file, FileUntrusted: true}, nil
		}
		if !strings.Contains(joined, "{{") {
			var value interface{}
			if err := json.Unmarshal([]byte(joined), &value); err != nil {
				return nil, fmt.Errorf("--json value is not valid JSON: %w", err)
			}
		}
		return &model.Body{Type: "json", Text: &joined}, nil
	case "form":
		return &model.Body{Type: "multipart", Multipart: append([]model.MultipartField(nil), p.forms...)}, nil
	default:
		return nil, fmt.Errorf("unsupported body mode %q", p.mode)
	}
}

func (p *curlParser) joinData(forGet bool) (string, error) {
	var b strings.Builder
	for i, data := range p.data {
		if data.file != "" {
			if forGet {
				return "", fmt.Errorf("file-backed data cannot be used with --get")
			}
			if len(p.data) != 1 {
				return "", fmt.Errorf("repeated data cannot combine file and inline values")
			}
			continue
		}
		if p.mode == "json" {
			b.WriteString(data.value)
		} else {
			if i > 0 {
				b.WriteByte('&')
			}
			b.WriteString(data.value)
		}
	}
	return b.String(), nil
}

func (p *curlParser) warn(path, message string) {
	p.warnings = append(p.warnings, Warning{Code: "lossy-curl", Path: path, Message: message})
}

func curlEntries(headers [][2]string) []model.Entry {
	entries := make([]model.Entry, len(headers))
	for i, header := range headers {
		entries[i] = model.Entry{Key: header[0], Value: header[1], Enabled: true}
	}
	return entries
}

func addCurlHeader(headers []model.Entry, name, value string) []model.Entry {
	for _, header := range headers {
		if strings.EqualFold(header.Key, name) {
			return headers
		}
	}
	return append(headers, model.Entry{Key: name, Value: value, Enabled: true})
}

func validateCurlURL(raw string) error {
	if strings.Contains(raw, "{{") {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("URL must be absolute http or https, got %q", raw)
	}
	if strings.ContainsAny(raw, "{}") {
		return fmt.Errorf("URL globbing is unsupported")
	}
	return nil
}

func appendCurlQuery(rawURL, query string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("invalid URL for --get data")
	}
	if u.RawQuery == "" {
		u.RawQuery = query
		u.ForceQuery = true
	} else if query != "" {
		u.RawQuery += "&" + query
	}
	return u.String(), nil
}

func validCurlToken(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !(r == '!' || r == '#' || r == '$' || r == '%' || r == '&' || r == '\'' || r == '*' || r == '+' || r == '-' || r == '.' || r == '^' || r == '_' || r == '`' || r == '|' || r == '~' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
			return false
		}
	}
	return true
}

func boolPointer(value bool) *bool { return &value }

func tokenizeCurl(source string) ([]string, error) {
	var tokens []string
	var word strings.Builder
	var single, double, have, started bool
	flush := func() {
		if !have {
			return
		}
		tokens = append(tokens, word.String())
		word.Reset()
		have = false
		started = true
	}
	for i := 0; i < len(source); i++ {
		c := source[i]
		if single {
			if c == '\'' {
				single = false
			} else {
				word.WriteByte(c)
			}
			continue
		}
		if double {
			switch c {
			case '"':
				double = false
			case '$', '`':
				return nil, fmt.Errorf("shell expansion or substitution is unsupported")
			case '\\':
				if i+1 >= len(source) {
					return nil, fmt.Errorf("unterminated escape")
				}
				next := source[i+1]
				if next == '\n' {
					i++
					continue
				}
				if next == '\r' && i+2 < len(source) && source[i+2] == '\n' {
					i += 2
					continue
				}
				if strings.ContainsRune("$`\\\"", rune(next)) {
					word.WriteByte(next)
					have = true
					i++
				} else {
					word.WriteByte('\\')
					have = true
				}
			default:
				word.WriteByte(c)
				have = true
			}
			continue
		}
		switch c {
		case '\'', '"':
			have = true
			if c == '\'' {
				single = true
			} else {
				double = true
			}
		case '\\':
			if i+1 >= len(source) {
				return nil, fmt.Errorf("unterminated escape")
			}
			next := source[i+1]
			if next == '\n' {
				i++
				continue
			}
			if next == '\r' && i+2 < len(source) && source[i+2] == '\n' {
				i += 2
				continue
			}
			word.WriteByte(next)
			have = true
			i++
		case ' ', '\t', '\r', '\v', '\f':
			flush()
		case '\n':
			flush()
			if started && curlHasCommandAfter(source, i+1) {
				return nil, fmt.Errorf("multiple commands are not allowed")
			}
		case '#':
			if !have {
				for i+1 < len(source) && source[i+1] != '\n' {
					i++
				}
				continue
			}
			word.WriteByte(c)
			have = true
		case ';', '|', '&', '<', '>', '(', ')':
			return nil, fmt.Errorf("active shell construct %q is unsupported", string(c))
		case '$', '`':
			return nil, fmt.Errorf("shell expansion or substitution is unsupported")
		case '{', '}':
			return nil, fmt.Errorf("shell expansion is unsupported")
		case '*', '[':
			return nil, fmt.Errorf("shell pathname expansion is unsupported")
		case '~':
			if !have {
				return nil, fmt.Errorf("shell tilde expansion is unsupported")
			}
			word.WriteByte(c)
			have = true
		default:
			word.WriteByte(c)
			have = true
		}
	}
	if single || double {
		return nil, fmt.Errorf("unterminated quote")
	}
	flush()
	if len(tokens) == 0 {
		return nil, fmt.Errorf("empty command")
	}
	return tokens, nil
}

func curlHasCommandAfter(source string, pos int) bool {
	for pos < len(source) {
		for pos < len(source) && (source[pos] == ' ' || source[pos] == '\t' || source[pos] == '\r' || source[pos] == '\v' || source[pos] == '\f' || source[pos] == '\n') {
			pos++
		}
		if pos >= len(source) {
			return false
		}
		if source[pos] == '#' {
			for pos < len(source) && source[pos] != '\n' {
				pos++
			}
			continue
		}
		return true
	}
	return false
}
