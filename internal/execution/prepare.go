package execution

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"req/internal/model"
)

// httpToken matches the RFC 9110 token character set, used for HTTP methods.
var httpToken = regexp.MustCompile(`^[!#$%&'*+\-.^_` + "`" + `|~0-9A-Za-z]+$`)

// placeholder matches an unresolved {{variable}} reference.
var placeholder = regexp.MustCompile(`\{\{[^{}]*\}\}`)

// Prepare merges saved and overrides into one Outgoing request. Only enabled
// saved entries are used; override entries are appended after the saved ones.
//
// Method: override wins, else saved; upper-cased and validated as an HTTP
// token. URL: override wins, else saved; must be absolute http or https.
// Body: the override body replaces the saved one when BodyMode is set;
// otherwise the saved body is used with Type raw→text or json→text, while
// "none"/nil means no body and "urlencoded"/"multipart" or a file reference
// is not supported for execution yet. A Content-Type default (json→
// application/json, raw→text/plain) is applied only when no explicit
// Content-Type header exists (case-insensitive) among the merged headers.
// Any remaining {{...}} placeholder in method, URL, headers or body fails
// with an error naming the placeholder — a literal placeholder is never
// sent. Policy is copied through verbatim.
func Prepare(saved model.Request, ov Overrides, pol Policy) (Outgoing, error) {
	method := ov.Method
	if method == "" {
		method = saved.Method
	}
	if m := placeholder.FindString(method); m != "" {
		return Outgoing{}, errUnresolved(m, "method")
	}
	method = strings.ToUpper(method)
	if !httpToken.MatchString(method) {
		return Outgoing{}, fmt.Errorf("invalid HTTP method %q", method)
	}

	rawURL := ov.URL
	if rawURL == "" {
		rawURL = saved.URL
	}
	if m := placeholder.FindString(rawURL); m != "" {
		return Outgoing{}, errUnresolved(m, "URL")
	}
	if u, err := url.Parse(rawURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return Outgoing{}, fmt.Errorf("URL must be absolute http or https, got %q", rawURL)
	}

	headers := append(savedEntries(saved.Headers), ov.Headers...)
	for _, h := range headers {
		if m := placeholder.FindString(h[0]); m != "" {
			return Outgoing{}, errUnresolved(m, fmt.Sprintf("header %q", h[0]))
		}
		if m := placeholder.FindString(h[1]); m != "" {
			return Outgoing{}, errUnresolved(m, fmt.Sprintf("header %q value", h[0]))
		}
	}

	queries := append(savedEntries(saved.Query), ov.Queries...)

	body, bodyKind, err := resolveBody(saved, ov)
	if err != nil {
		return Outgoing{}, err
	}
	if m := placeholder.FindString(string(body)); m != "" {
		return Outgoing{}, errUnresolved(m, "body")
	}

	if body != nil && !hasHeader(headers, "Content-Type") {
		if bodyKind == "json" {
			headers = append(headers, [2]string{"Content-Type", "application/json"})
		} else {
			headers = append(headers, [2]string{"Content-Type", "text/plain"})
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
	}, nil
}

// resolveBody picks the outgoing body bytes: the override body when a mode is
// given, otherwise the saved body. It returns the body (nil = no body) and
// the body kind ("raw" or "json") that drives the Content-Type default.
func resolveBody(saved model.Request, ov Overrides) ([]byte, string, error) {
	if ov.BodyMode != "" {
		switch ov.BodyMode {
		case "raw", "json":
			return ov.Body, ov.BodyMode, nil
		default:
			return nil, "", fmt.Errorf("unsupported override body mode %q (want \"raw\" or \"json\")", ov.BodyMode)
		}
	}
	b := saved.Body
	switch {
	case b == nil || b.Type == "none":
		return nil, "", nil
	case b.File != "":
		return nil, "", fmt.Errorf("body from file %q is not supported for execution yet", b.File)
	case b.Type == "raw" || b.Type == "json":
		return []byte(b.Text), b.Type, nil
	case b.Type == "urlencoded" || b.Type == "multipart":
		return nil, "", fmt.Errorf("%s body is not supported for execution yet", b.Type)
	default:
		return nil, "", fmt.Errorf("unknown body type %q", b.Type)
	}
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

// errUnresolved reports an unresolved {{variable}} placeholder found in the
// named field.
func errUnresolved(ph, where string) error {
	return fmt.Errorf("unresolved variable %s in %s (variables arrive with environment support; none are defined in this execution)", ph, where)
}
