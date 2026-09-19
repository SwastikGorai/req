package execution

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"req/internal/httpclient"
	"req/internal/model"
	"req/internal/scripting"
)

// httpToken matches the RFC 9110 token character set, used for HTTP methods.
var httpToken = regexp.MustCompile(`^[!#$%&'*+\-.^_` + "`" + `|~0-9A-Za-z]+$`)

// Prepare merges saved and overrides into one Outgoing request. Only enabled
// saved entries are used; override entries are appended after the saved ones.
//
// Method: override wins, else saved; upper-cased and validated as an HTTP
// token. URL: override wins, else saved; must be absolute http or https.
// Variables resolve after structural overrides, in a single pass. Missing
// variables and invalid resolved JSON fail before HTTP; explicit Authorization
// headers take precedence over generated auth.
func Prepare(saved model.Request, ov Overrides, pol Policy) (Outgoing, error) {
	return ResolveRequest(MergeOverrides(saved, ov), pol)
}

// MergeOverrides builds the mutable execution copy that pre scripts may
// modify before ResolveRequest runs. Only enabled saved entries are used;
// override entries are appended after the saved ones. Method and URL keep
// their unresolved values. The chosen body definition is deep-copied so
// script mutations and in-place resolution never touch the saved request.
func MergeOverrides(saved model.Request, ov Overrides) *scripting.ExecRequest {
	method := ov.Method
	if method == "" {
		method = saved.Method
	}
	rawURL := ov.URL
	if rawURL == "" {
		rawURL = saved.URL
	}
	auth := saved.Auth
	if ov.Auth != nil {
		auth = ov.Auth
	}
	body := saved.Body
	if ov.Body != nil {
		body = ov.Body
	}
	var copy *model.Body
	if body != nil {
		b := *body
		copy = &b
	}
	return &scripting.ExecRequest{
		Method:  method,
		URL:     rawURL,
		Headers: append(savedEntries(saved.Headers), ov.Headers...),
		Queries: append(savedEntries(saved.Query), ov.Queries...),
		Auth:    auth,
		Body:    copy,
	}
}

// ResolveRequest validates, resolves variables and builds the outgoing HTTP
// request from the execution copy m. Header, query and body values resolve in
// place, so pm.request mutations from pre scripts and {{references}} feed the
// same copy; after it returns, an inline raw/JSON m.Body.Text holds the
// resolved text. Explicit Authorization headers take precedence over
// generated auth.
func ResolveRequest(m *scripting.ExecRequest, pol Policy) (Outgoing, error) {
	method, err := pol.Variables.ResolveString(m.Method, "method")
	if err != nil {
		return Outgoing{}, err
	}
	method = strings.ToUpper(method)
	if !httpToken.MatchString(method) {
		return Outgoing{}, fmt.Errorf("invalid HTTP method %q", method)
	}

	rawURL, err := pol.Variables.ResolveString(m.URL, "URL")
	if err != nil {
		return Outgoing{}, err
	}
	if u, err := url.Parse(rawURL); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return Outgoing{}, fmt.Errorf("URL must be absolute http or https, got %q", rawURL)
	}

	headers, queries := m.Headers, m.Queries
	for _, group := range []struct {
		location string
		entries  [][2]string
	}{{"header", headers}, {"query", queries}} {
		location, entries := group.location, group.entries
		for i := range entries {
			for j := range entries[i] {
				entries[i][j], err = pol.Variables.ResolveString(entries[i][j], fmt.Sprintf("%s[%d][%d]", location, i, j))
				if err != nil {
					return Outgoing{}, err
				}
			}
			if entries[i][0] == "" {
				return Outgoing{}, fmt.Errorf("empty %s key", location)
			}
			if location == "header" && (!httpToken.MatchString(entries[i][0]) || strings.ContainsAny(entries[i][1], "\r\n")) {
				return Outgoing{}, fmt.Errorf("invalid header at index %d", i)
			}
		}
	}

	// Copy before appending generated headers so m keeps its exact slice.
	headers = append([][2]string(nil), headers...)

	auth := m.Auth
	if auth != nil && !hasHeader(headers, "Authorization") {
		var value string
		switch auth.Type {
		case "inherit", "none":
		case "bearer":
			token, err := pol.Variables.ResolveString(auth.Token, "auth token")
			if err != nil {
				return Outgoing{}, err
			}
			value = "Bearer " + token
		case "basic":
			user, err := pol.Variables.ResolveString(auth.Username, "auth username")
			if err != nil {
				return Outgoing{}, err
			}
			password, err := pol.Variables.ResolveString(auth.Password, "auth password")
			if err != nil {
				return Outgoing{}, err
			}
			if strings.Contains(user, ":") {
				return Outgoing{}, fmt.Errorf("basic username cannot contain a colon")
			}
			value = "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
		default:
			return Outgoing{}, fmt.Errorf("unsupported auth type %q", auth.Type)
		}
		if strings.ContainsAny(value, "\r\n") {
			return Outgoing{}, fmt.Errorf("invalid auth header")
		}
		if value != "" {
			headers = append(headers, [2]string{"Authorization", value})
		}
	}

	definition, err := resolveBody(m, pol)
	if err != nil {
		return Outgoing{}, err
	}
	contentType, typeIndex := "", -1
	for i, h := range headers {
		if strings.EqualFold(h[0], "Content-Type") {
			if typeIndex >= 0 && definition != nil && definition.Type == "multipart" {
				return Outgoing{}, fmt.Errorf("multipart body requires a single Content-Type header")
			}
			if typeIndex < 0 {
				contentType, typeIndex = h[1], i
			}
		}
	}
	body, err := httpclient.BuildBody(definition, contentType)
	if err != nil {
		return Outgoing{}, err
	}
	if body != nil {
		if typeIndex < 0 {
			headers = append(headers, [2]string{"Content-Type", body.ContentType})
		} else if definition.Type == "multipart" {
			headers[typeIndex][1] = body.ContentType
		}
	}

	finalURL, err := appendQueries(rawURL, queries)
	if err != nil {
		return Outgoing{}, err
	}

	return Outgoing{
		Method:          method,
		URL:             finalURL,
		Headers:         headers,
		Body:            body,
		Timeout:         pol.Timeout,
		InsecureTLS:     pol.InsecureTLS,
		FollowRedirects: pol.FollowRedirects,
		FailOnHTTPError: pol.FailOnHTTPError,
		Output:          pol.Output,
	}, nil
}

// savedEntries flattens the enabled stored entries into ordered key/value
// pairs; disabled entries are dropped.
func savedEntries(entries []model.Entry) [][2]string {
	out := make([][2]string, 0, len(entries))
	for _, e := range entries {
		if e.Enabled {
			out = append(out, [2]string{e.Key, e.Value})
		}
	}
	return out
}

// hasHeader reports whether headers already carry an entry named name
// (case-insensitive).
func hasHeader(headers [][2]string, name string) bool {
	for _, h := range headers {
		if strings.EqualFold(h[0], name) {
			return true
		}
	}
	return false
}

// appendQueries appends query entries without re-encoding the components the
// URL already carries. Repeated keys and empty values survive as-is.
func appendQueries(rawURL string, entries [][2]string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	for _, kv := range entries {
		sep := "&"
		if u.RawQuery == "" {
			sep = ""
		}
		u.RawQuery += sep + url.QueryEscape(kv[0]) + "=" + url.QueryEscape(kv[1])
	}
	return u.String(), nil
}
