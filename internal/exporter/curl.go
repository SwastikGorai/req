// Package exporter converts native saved requests to shell-safe cURL text.
package exporter

import (
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/SwastikGorai/req/internal/model"
	"github.com/SwastikGorai/req/internal/variables"
)

// Options controls cURL export. Values remain exactly as saved unless
// Resolve is set; resolved output requires Variables and may expose secrets.
type Options struct {
	Resolve   bool
	Variables *variables.Scope
	Strict    bool
}

// Result is one cURL command and warnings about omitted or lossy behavior.
type Result struct {
	Command  string
	Warnings []Warning
}

// Warning identifies behavior that cURL output cannot represent faithfully.
type Warning struct {
	Code    string
	Path    string
	Message string
}

func (w Warning) Error() string {
	if w.Path == "" {
		return w.Message
	}
	return w.Path + ": " + w.Message
}

// ErrStrict marks an export rejected before command output was emitted.
var ErrStrict = errors.New("strict export rejected non-representable behavior")

// StrictError reports warnings that prevented a strict export.
type StrictError struct {
	Warnings []Warning
}

func (e *StrictError) Error() string {
	if len(e.Warnings) == 0 {
		return ErrStrict.Error()
	}
	parts := make([]string, len(e.Warnings))
	for i, warning := range e.Warnings {
		parts[i] = warning.Error()
	}
	return ErrStrict.Error() + ": " + strings.Join(parts, "; ")
}

func (e *StrictError) Unwrap() error { return ErrStrict }

// SortedWarnings returns a stable copy for deterministic CLI output.
func SortedWarnings(warnings []Warning) []Warning {
	out := append([]Warning(nil), warnings...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		if out[i].Code != out[j].Code {
			return out[i].Code < out[j].Code
		}
		return out[i].Message < out[j].Message
	})
	return out
}

// ExportCurl formats one native request without reading body files or running
// scripts. Strict mode returns a StrictError and an empty command whenever a
// warning was found.
func ExportCurl(request model.Request, opts Options) (Result, error) {
	f := curlFormatter{opts: opts}
	args, err := f.format(request)
	if err != nil {
		return Result{Warnings: f.warnings}, err
	}
	result := Result{Command: strings.Join(args, " "), Warnings: f.warnings}
	if opts.Strict && len(result.Warnings) > 0 {
		result.Command = ""
		return result, &StrictError{Warnings: result.Warnings}
	}
	return result, nil
}

type curlFormatter struct {
	opts     Options
	warnings []Warning
}

func (f *curlFormatter) format(request model.Request) ([]string, error) {
	resolve := f.resolve
	method, err := resolve(request.Method, "method")
	if err != nil {
		return nil, err
	}
	rawURL, err := resolve(request.URL, "URL")
	if err != nil {
		return nil, err
	}
	if rawURL == "" {
		return nil, errors.New("URL is empty")
	}

	if request.Import != nil {
		for i, unsupported := range request.Import.Unsupported {
			f.warn("import", fmt.Sprintf("import.unsupported[%d]", i), "saved request has unsupported behavior: "+unsupported)
		}
		for i, warning := range request.Import.Warnings {
			f.warn("import-warning", fmt.Sprintf("import.warnings[%d]", i), warning)
		}
	}
	if hasEnabledScripts(request.Scripts) {
		f.warn("scripts-omitted", "scripts", "enabled scripts are omitted; cURL export never executes scripts")
	}

	body := request.Body
	if body != nil {
		if err := body.Validate(); err != nil {
			return nil, err
		}
	}

	resolvedHeaders := make([][2]string, 0, len(request.Headers))
	args := []string{"curl"}
	bodyPresent := body != nil && body.Type != "none"
	if !bodyPresent && strings.EqualFold(method, "GET") {
		// cURL's default method is GET, so no -X is needed.
	} else {
		args = append(args, "-X", quote(method))
	}
	if request.FollowRedirects == nil || *request.FollowRedirects {
		args = append(args, "-L")
	}
	if request.InsecureTLS {
		args = append(args, "-k")
	}
	for i, header := range request.Headers {
		if !header.Enabled {
			continue
		}
		key, err := resolve(header.Key, fmt.Sprintf("headers[%d].key", i))
		if err != nil {
			return nil, err
		}
		value, err := resolve(header.Value, fmt.Sprintf("headers[%d].value", i))
		if err != nil {
			return nil, err
		}
		if strings.ContainsAny(key+value, "\x00\r\n") {
			f.warn("invalid-header", fmt.Sprintf("headers[%d]", i), "header contains a NUL or newline and cannot be sent faithfully")
		}
		resolvedHeaders = append(resolvedHeaders, [2]string{key, value})
		args = append(args, "-H", quote(key+": "+value))
	}

	if request.Auth != nil && !hasHeader(resolvedHeaders, "Authorization") {
		switch request.Auth.Type {
		case "", "inherit", "none":
		case "basic":
			user, err := resolve(request.Auth.Username, "auth username")
			if err != nil {
				return nil, err
			}
			password, err := resolve(request.Auth.Password, "auth password")
			if err != nil {
				return nil, err
			}
			if strings.Contains(user, ":") {
				f.warn("invalid-auth", "auth.username", "basic username contains a colon and cannot be represented")
			}
			if strings.ContainsAny(user+password, "\x00\r\n") {
				f.warn("invalid-auth", "auth", "basic credentials contain a NUL or newline and cannot be sent faithfully")
			}
			args = append(args, "-u", quote(user+":"+password))
		case "bearer":
			token, err := resolve(request.Auth.Token, "auth token")
			if err != nil {
				return nil, err
			}
			if strings.ContainsAny(token, "\x00\r\n") {
				f.warn("invalid-auth", "auth.token", "bearer token contains a NUL or newline and cannot be sent faithfully")
			}
			args = append(args, "-H", quote("Authorization: Bearer "+token))
		default:
			f.warn("unsupported-auth", "auth", fmt.Sprintf("auth type %q is omitted", request.Auth.Type))
		}
	}

	if bodyPresent {
		bodyArgs, err := f.bodyArgs(body, resolve)
		if err != nil {
			return nil, err
		}
		args = append(args, bodyArgs...)
		if body.Type == "multipart" && hasHeader(resolvedHeaders, "Content-Type") {
			f.warn("multipart-content-type", "body", "explicit multipart Content-Type boundaries cannot be preserved with cURL -F")
		}
		if (body.Type == "raw" || body.Type == "json") && !hasHeader(resolvedHeaders, "Content-Type") {
			contentType := "text/plain"
			if body.Type == "json" {
				contentType = "application/json"
			} else if body.File != "" {
				contentType = "application/octet-stream"
			}
			args = append(args, "-H", quote("Content-Type: "+contentType))
		}
	}

	for i, entry := range request.Query {
		if !entry.Enabled {
			continue
		}
		key, err := resolve(entry.Key, fmt.Sprintf("query[%d].key", i))
		if err != nil {
			return nil, err
		}
		value, err := resolve(entry.Value, fmt.Sprintf("query[%d].value", i))
		if err != nil {
			return nil, err
		}
		rawURL = appendQuery(rawURL, formComponent(key)+"="+formComponent(value))
	}
	args = append(args, quote(rawURL))
	return args, nil
}

func (f *curlFormatter) bodyArgs(body *model.Body, resolve func(string, string) (string, error)) ([]string, error) {
	switch body.Type {
	case "raw", "json":
		if body.File != "" {
			path, err := resolve(body.File, "body file")
			if err != nil {
				return nil, err
			}
			flag := "--data-binary"
			if body.Type == "json" {
				flag = "--json"
			}
			return []string{flag, quote("@" + path)}, nil
		}
		text := ""
		if body.Text != nil {
			var err error
			text, err = resolve(*body.Text, "body")
			if err != nil {
				return nil, err
			}
		}
		flag := "--data-raw"
		if body.Type == "json" {
			flag = "--json"
		}
		return []string{flag, quote(text)}, nil
	case "urlencoded":
		var parts []string
		for i, entry := range body.URLEncoded {
			if !entry.Enabled {
				continue
			}
			key, err := resolve(entry.Key, fmt.Sprintf("urlencoded[%d].key", i))
			if err != nil {
				return nil, err
			}
			value, err := resolve(entry.Value, fmt.Sprintf("urlencoded[%d].value", i))
			if err != nil {
				return nil, err
			}
			parts = append(parts, formComponent(key)+"="+formComponent(value))
		}
		return []string{"--data-raw", quote(strings.Join(parts, "&"))}, nil
	case "multipart":
		var args []string
		fieldCount := 0
		for i, field := range body.Multipart {
			if !field.Enabled {
				continue
			}
			key, err := resolve(field.Key, fmt.Sprintf("multipart[%d].key", i))
			if err != nil {
				return nil, err
			}
			if field.File != "" {
				path, err := resolve(field.File, fmt.Sprintf("multipart[%d].file", i))
				if err != nil {
					return nil, err
				}
				value := key + "=@" + path
				if field.ContentType != "" {
					contentType, err := resolve(field.ContentType, fmt.Sprintf("multipart[%d].content_type", i))
					if err != nil {
						return nil, err
					}
					value += ";type=" + contentType
				}
				if field.Filename != "" {
					filename, err := resolve(field.Filename, fmt.Sprintf("multipart[%d].filename", i))
					if err != nil {
						return nil, err
					}
					value += ";filename=" + filename
				}
				if strings.Contains(path, ";") {
					f.warn("multipart-file", fmt.Sprintf("multipart[%d].file", i), "file path contains ';' and may be parsed as cURL form metadata")
				}
				args = append(args, "-F", quote(value))
				fieldCount++
				continue
			}
			text := ""
			if field.Value != nil {
				text, err = resolve(*field.Value, fmt.Sprintf("multipart[%d].value", i))
				if err != nil {
					return nil, err
				}
			}
			// --form-string keeps @, ';' and other form syntax literal.
			args = append(args, "--form-string", quote(key+"="+text))
			fieldCount++
		}
		if fieldCount == 0 {
			f.warn("multipart-empty", "body", "empty multipart bodies cannot be represented without a form field")
		}
		return args, nil
	default:
		f.warn("unsupported-body", "body", fmt.Sprintf("body type %q is omitted", body.Type))
		return nil, nil
	}
}

func (f *curlFormatter) resolve(value, location string) (string, error) {
	if !f.opts.Resolve {
		return value, nil
	}
	if f.opts.Variables == nil {
		return "", errors.New("resolved export requires a variable scope")
	}
	return f.opts.Variables.ResolveString(value, location)
}

func (f *curlFormatter) warn(code, path, message string) {
	f.warnings = append(f.warnings, Warning{Code: code, Path: path, Message: message})
}

func hasEnabledScripts(scripts *model.Scripts) bool {
	if scripts == nil {
		return false
	}
	for _, script := range append(append([]model.Script(nil), scripts.PreRequest...), scripts.PostResponse...) {
		if script.Enabled {
			return true
		}
	}
	return false
}

func hasHeader(headers [][2]string, name string) bool {
	for _, header := range headers {
		if strings.EqualFold(header[0], name) {
			return true
		}
	}
	return false
}

func quote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func formComponent(value string) string {
	var out strings.Builder
	for len(value) > 0 {
		start := strings.Index(value, "{{")
		if start < 0 {
			out.WriteString(url.QueryEscape(value))
			break
		}
		out.WriteString(url.QueryEscape(value[:start]))
		end := strings.Index(value[start+2:], "}}")
		if end < 0 {
			out.WriteString(url.QueryEscape(value[start:]))
			break
		}
		end += start + 4
		out.WriteString(value[start:end])
		value = value[end:]
	}
	return out.String()
}

func appendQuery(rawURL, query string) string {
	fragment := ""
	if index := strings.IndexByte(rawURL, '#'); index >= 0 {
		fragment, rawURL = rawURL[index:], rawURL[:index]
	}
	separator := "?"
	if strings.Contains(rawURL, "?") {
		separator = "&"
		if strings.HasSuffix(rawURL, "?") || strings.HasSuffix(rawURL, "&") {
			separator = ""
		}
	}
	return rawURL + separator + query + fragment
}
