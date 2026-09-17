package importer

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"req/internal/store"
)

func TestCurlQuotedLiteral(t *testing.T) {
	result, err := ParseCurl([]byte("curl -X POST -H 'X-Literal: ;|&$()' --data 'a=one & two; $HOME' https://example.test/api -H 'X-Next: quoted value'"), Options{Source: "request.curl"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Request.Method != "POST" || result.Request.URL != "https://example.test/api" {
		t.Fatalf("request = %+v", result.Request)
	}
	if got := result.Request.Headers[0]; got.Key != "X-Literal" || got.Value != ";|&$()" {
		t.Fatalf("quoted header = %+v", got)
	}
	if got := result.Request.Body; got == nil || got.Text == nil || *got.Text != "a=one & two; $HOME" {
		t.Fatalf("quoted body = %+v", got)
	}
	if result.Request.Import == nil || result.Request.Import.Source != "curl" || result.Request.Import.Path != "request.curl" {
		t.Fatalf("provenance = %+v", result.Request.Import)
	}
}

func TestCurlContinuationAndJson(t *testing.T) {
	result, err := ParseCurl([]byte("curl --json '{\"name\":' \\\n  --json ' \"literal\"}' https://example.test"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Request.Method != "POST" || result.Request.Body == nil || result.Request.Body.Type != "json" || result.Request.Body.Text == nil || *result.Request.Body.Text != "{\"name\": \"literal\"}" {
		t.Fatalf("json continuation = %+v", result.Request)
	}
	if len(result.Request.Headers) != 2 || result.Request.Headers[0].Key != "Content-Type" || result.Request.Headers[1].Key != "Accept" {
		t.Fatalf("json headers = %+v", result.Request.Headers)
	}
}

func TestCurlRejectSubstitution(t *testing.T) {
	for _, command := range []string{
		"curl https://example.test/$(touch pwned)",
		"curl \"https://example.test/$(touch pwned)\"",
		"curl https://example.test/`touch pwned`",
		"curl https://example.test/$HOME",
	} {
		if _, err := ParseCurl([]byte(command), Options{}); err == nil || !strings.Contains(err.Error(), "shell") {
			t.Fatalf("ParseCurl(%q) error = %v, want shell rejection", command, err)
		}
	}
}

func TestCurlNoLocation(t *testing.T) {
	without, err := ParseCurl([]byte("curl https://example.test"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	with, err := ParseCurl([]byte("curl -L https://example.test"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if without.Request.FollowRedirects == nil || *without.Request.FollowRedirects {
		t.Fatalf("without -L FollowRedirects = %v, want false", without.Request.FollowRedirects)
	}
	if with.Request.FollowRedirects == nil || !*with.Request.FollowRedirects {
		t.Fatalf("with -L FollowRedirects = %v, want true", with.Request.FollowRedirects)
	}
}

func TestCurlRequestMapping(t *testing.T) {
	result, err := ParseCurl([]byte("curl -X PATCH -H 'X-Test: one' -H 'X-Test: two' -d 'a=1' -d 'b=2' -u user:pa:ss -k https://example.test"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	r := result.Request
	if r.Method != "PATCH" || !r.InsecureTLS || r.Auth == nil || r.Auth.Username != "user" || r.Auth.Password != "pa:ss" {
		t.Fatalf("request mapping = %+v", r)
	}
	if got := []string{r.Headers[0].Value, r.Headers[1].Value}; !reflect.DeepEqual(got, []string{"one", "two"}) {
		t.Fatalf("repeated headers = %v", got)
	}
	if r.Headers[2].Key != "Content-Type" || r.Headers[2].Value != "application/x-www-form-urlencoded" {
		t.Fatalf("data content type = %+v", r.Headers)
	}
	if r.Body == nil || r.Body.Text == nil || *r.Body.Text != "a=1&b=2" {
		t.Fatalf("repeated data = %+v", r.Body)
	}
}

func TestCurlGetDataAndFormFiles(t *testing.T) {
	result, err := ParseCurl([]byte("curl -G --data 'q=a%20b' --data 'empty=' https://example.test/api?old=1"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Request.Method != "GET" || result.Request.Body != nil || result.Request.URL != "https://example.test/api?old=1&q=a%20b&empty=" {
		t.Fatalf("--get mapping = %+v", result.Request)
	}
	result, err = ParseCurl([]byte("curl -F 'upload=@missing.bin;type=application/octet-stream;filename=payload.bin' https://example.test"), Options{})
	if err != nil {
		t.Fatal(err)
	}
	field := result.Request.Body.Multipart[0]
	if field.File != "missing.bin" || !field.FileUntrusted || field.ContentType != "application/octet-stream" || field.Filename != "payload.bin" {
		t.Fatalf("form file = %+v", field)
	}
}

func TestCurlStrictDataFileWarningNoWrite(t *testing.T) {
	result, err := ParseCurl([]byte("curl --data @missing.bin https://example.test"), Options{Strict: true})
	var strict *StrictError
	if !errors.As(err, &strict) || len(result.Warnings) != 1 {
		t.Fatalf("strict result = %+v, err=%v", result, err)
	}
	ws, _, err := store.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_, err = ImportCurl(context.Background(), ws, []byte("curl --data @missing.bin https://example.test"), Options{Name: "API/Request", Strict: true})
	if !errors.As(err, &strict) {
		t.Fatalf("strict import err = %v", err)
	}
	if collections, listErr := ws.ListCollections(context.Background()); listErr != nil || len(collections) != 0 {
		t.Fatalf("strict import wrote collections = %d, err=%v", len(collections), listErr)
	}
}

func TestCurlRejectUnsafeCombinations(t *testing.T) {
	for _, command := range []string{
		"curl --bogus https://example.test",
		"curl https://one.test https://two.test",
		"curl --data x --form y=z https://example.test",
		"curl -G --json '{}' https://example.test",
		"curl https://example.test; echo bad",
		"curl https://example.test | cat",
		"curl https://example.test > out",
	} {
		if _, err := ParseCurl([]byte(command), Options{}); err == nil {
			t.Fatalf("ParseCurl(%q) succeeded, want rejection", command)
		}
	}
}

func TestCurlImportStoresRequest(t *testing.T) {
	ws, _, err := store.Init(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.CreateCollection(context.Background(), "API"); err != nil {
		t.Fatal(err)
	}
	result, err := ImportCurl(context.Background(), ws, []byte("curl -L https://example.test"), Options{Name: "API/Ping", Source: "sample.curl"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Request.Import == nil || result.Request.Import.Path != "sample.curl" {
		t.Fatalf("result provenance = %+v", result.Request.Import)
	}
	resolved, err := ws.ResolvePath(context.Background(), "API/Ping")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Item == nil || resolved.Item.Request.FollowRedirects == nil || !*resolved.Item.Request.FollowRedirects {
		t.Fatalf("stored request = %+v", resolved.Item)
	}
}
