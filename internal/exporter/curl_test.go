package exporter

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/SwastikGorai/req/internal/importer"
	"github.com/SwastikGorai/req/internal/model"
	"github.com/SwastikGorai/req/internal/variables"
)

func TestCurlRoundTrip(t *testing.T) {
	body := `{"name":"Ada"}`
	request := model.Request{
		Method:  "PATCH",
		URL:     "https://example.test/items?old=1",
		Query:   []model.Entry{{Key: "q", Value: "a b", Enabled: true}},
		Headers: []model.Entry{{Key: "X-Test", Value: "one 'two'", Enabled: true}},
		Body:    &model.Body{Type: "json", Text: &body},
	}
	result, err := ExportCurl(request, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Command, "q=a+b") {
		t.Fatalf("command = %q", result.Command)
	}
	parsed, err := importer.ParseCurl([]byte(result.Command), importer.Options{})
	if err != nil {
		t.Fatalf("import exported command %q: %v", result.Command, err)
	}
	if parsed.Request.Method != request.Method || parsed.Request.URL != "https://example.test/items?old=1&q=a+b" {
		t.Fatalf("round-trip request = %+v", parsed.Request)
	}
	if len(parsed.Request.Headers) < 1 || parsed.Request.Headers[0].Value != "one 'two'" {
		t.Fatalf("round-trip headers = %+v", parsed.Request.Headers)
	}
	if parsed.Request.Body == nil || parsed.Request.Body.Text == nil || *parsed.Request.Body.Text != body {
		t.Fatalf("round-trip body = %+v", parsed.Request.Body)
	}
}

func TestCurlBodyRoundTrip(t *testing.T) {
	rawText := "name=Ada"
	jsonText := `{"name":"Ada"}`
	multipartText := "@literal;still-text"
	urlencodedText := "name=Ada+Lovelace&note=a%2Bb"
	cases := []struct {
		name          string
		body          *model.Body
		wantType      string
		wantText      *string
		wantFile      string
		wantMultipart []model.MultipartField
		wantHeader    string
	}{
		{name: "raw inline", body: &model.Body{Type: "raw", Text: &rawText}, wantType: "raw", wantText: &rawText},
		{name: "json inline", body: &model.Body{Type: "json", Text: &jsonText}, wantType: "json", wantText: &jsonText},
		{name: "raw file", body: &model.Body{Type: "raw", File: "payload.bin", FileUntrusted: true}, wantType: "raw", wantFile: "payload.bin"},
		{name: "json file", body: &model.Body{Type: "json", File: "payload.json", FileUntrusted: true}, wantType: "json", wantFile: "payload.json"},
		{
			name:     "urlencoded",
			body:     &model.Body{Type: "urlencoded", URLEncoded: []model.Entry{{Key: "name", Value: "Ada Lovelace", Enabled: true}, {Key: "note", Value: "a+b", Enabled: true}, {Key: "ignored", Value: "x", Enabled: false}}},
			wantType: "raw", wantText: &urlencodedText, wantHeader: "application/x-www-form-urlencoded",
		},
		{
			name:     "multipart literal",
			body:     &model.Body{Type: "multipart", Multipart: []model.MultipartField{{Key: "text", Value: &multipartText, Enabled: true}}},
			wantType: "multipart", wantMultipart: []model.MultipartField{{Key: "text", Value: &multipartText, Enabled: true}},
		},
		{
			name:     "multipart file",
			body:     &model.Body{Type: "multipart", Multipart: []model.MultipartField{{Key: "upload", File: "payload.bin", FileUntrusted: true, ContentType: "application/octet-stream", Filename: "payload.bin", Enabled: true}}},
			wantType: "multipart", wantMultipart: []model.MultipartField{{Key: "upload", File: "payload.bin", FileUntrusted: true, ContentType: "application/octet-stream", Filename: "payload.bin", Enabled: true}},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExportCurl(model.Request{Method: "POST", URL: "https://example.test/upload", Body: tt.body}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := importer.ParseCurl([]byte(result.Command), importer.Options{})
			if err != nil {
				t.Fatalf("import exported command %q: %v", result.Command, err)
			}
			if parsed.Request.Method != "POST" || parsed.Request.Body == nil || parsed.Request.Body.Type != tt.wantType {
				t.Fatalf("round-trip request = %+v", parsed.Request)
			}
			if tt.wantText != nil && (parsed.Request.Body.Text == nil || *parsed.Request.Body.Text != *tt.wantText) {
				t.Fatalf("round-trip text = %+v, want %q", parsed.Request.Body.Text, *tt.wantText)
			}
			if tt.wantFile != "" && (parsed.Request.Body.File != tt.wantFile || !parsed.Request.Body.FileUntrusted) {
				t.Fatalf("round-trip file = %+v, want untrusted %q", parsed.Request.Body, tt.wantFile)
			}
			if tt.wantMultipart != nil && !reflect.DeepEqual(parsed.Request.Body.Multipart, tt.wantMultipart) {
				t.Fatalf("round-trip multipart = %+v, want %+v", parsed.Request.Body.Multipart, tt.wantMultipart)
			}
			if tt.wantHeader != "" {
				found := false
				for _, header := range parsed.Request.Headers {
					if strings.EqualFold(header.Key, "Content-Type") && header.Value == tt.wantHeader {
						found = true
					}
				}
				if !found {
					t.Fatalf("round-trip headers = %+v", parsed.Request.Headers)
				}
			}
		})
	}
}

func TestCurlScriptsWarn(t *testing.T) {
	request := model.Request{
		Method:  "GET",
		URL:     "https://example.test",
		Scripts: &model.Scripts{PreRequest: []model.Script{{ID: "pre", Source: "pm.test('x', () => {})", Enabled: true}}},
	}
	result, err := ExportCurl(request, Options{})
	if err != nil || len(result.Warnings) != 1 || result.Command == "" {
		t.Fatalf("lenient result = %+v, err=%v", result, err)
	}
	if !strings.Contains(result.Warnings[0].Message, "never executes") {
		t.Fatalf("warning = %+v", result.Warnings[0])
	}
	strict, err := ExportCurl(request, Options{Strict: true})
	var strictErr *StrictError
	if !errors.As(err, &strictErr) || strict.Command != "" {
		t.Fatalf("strict result = %+v, err=%v", strict, err)
	}
}

func TestCurlResolveOptIn(t *testing.T) {
	request := model.Request{
		Method:  "GET",
		URL:     "https://example.test/{{path}}",
		Headers: []model.Entry{{Key: "Authorization", Value: "Bearer {{token}}", Enabled: true}},
	}
	scope := &variables.Scope{Environment: map[string]any{"path": "users", "token": "secret'"}}
	kept, err := ExportCurl(request, Options{})
	if err != nil || !strings.Contains(kept.Command, "{{path}}") || !strings.Contains(kept.Command, "{{token}}") {
		t.Fatalf("default export = %+v, err=%v", kept, err)
	}
	resolved, err := ExportCurl(request, Options{Resolve: true, Variables: scope})
	if err != nil || strings.Contains(resolved.Command, "{{") || !strings.Contains(resolved.Command, "secret'\\''") {
		t.Fatalf("resolved export = %+v, err=%v", resolved, err)
	}
}
