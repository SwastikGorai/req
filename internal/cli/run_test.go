package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/SwastikGorai/req/internal/model"
)

// recordedRequest is what a test server hands back to the test goroutine.
type recordedRequest struct {
	method, path, query string
	header              http.Header
	body                []byte
}

// collectionFileBytes returns the bytes of the single stored collection file.
func collectionFileBytes(t *testing.T) []byte {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(".req", "collections", "*.json"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("expected exactly one collection file, got %v (err: %v)", matches, err)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("reading collection file: %v", err)
	}
	return data
}

func TestSavedRunAfterReload(t *testing.T) {
	t.Chdir(t.TempDir())

	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")
	mustRun(t, exitSuccess, "folder", "create", "API/Auth", "--parents")

	rec := make(chan recordedRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec <- recordedRequest{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, header: r.Header.Clone(), body: body}
		_, _ = io.WriteString(w, "welcome, traveler")
	}))
	defer srv.Close()

	mustRun(t, exitSuccess, "request", "create", "API/Auth/Login",
		"--method", "POST", "--url", srv.URL+"/login",
		"--json", `{"u":"x"}`, "--header", "X-Test: 1")

	before := collectionFileBytes(t)

	// A fresh Run invocation stands in for a separate process: the saved
	// request is reloaded from disk by this call.
	stdout, stderr := mustRun(t, exitSuccess, "run", "API/Auth/Login")
	if stdout != "welcome, traveler" {
		t.Errorf("stdout = %q, want the server body", stdout)
	}
	if !strings.Contains(stderr, "POST "+srv.URL+"/login -> 200") {
		t.Errorf("stderr = %q, want the status line", stderr)
	}

	got := <-rec
	if got.method != http.MethodPost {
		t.Errorf("server saw method %q, want POST", got.method)
	}
	if got.path != "/login" {
		t.Errorf("server saw path %q, want /login", got.path)
	}
	if got.query != "" {
		t.Errorf("server saw query %q, want none", got.query)
	}
	if got.header.Get("X-Test") != "1" {
		t.Errorf("server saw header X-Test %q, want 1", got.header.Get("X-Test"))
	}
	if got.header.Get("Content-Type") != "application/json" {
		t.Errorf("server saw Content-Type %q, want the saved JSON body's default", got.header.Get("Content-Type"))
	}
	if string(got.body) != `{"u":"x"}` {
		t.Errorf("server saw body %q, want the saved JSON text", got.body)
	}

	if after := collectionFileBytes(t); !bytes.Equal(before, after) {
		t.Error("run rewrote the collection file")
	}
}

func TestRunOverrides(t *testing.T) {
	t.Chdir(t.TempDir())

	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")

	rec := make(chan recordedRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec <- recordedRequest{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, header: r.Header.Clone(), body: body}
		_, _ = io.WriteString(w, "overridden")
	}))
	defer srv.Close()

	mustRun(t, exitSuccess, "request", "create", "API/Go",
		"--method", "GET", "--url", srv.URL+"/go",
		"--query", "a=1", "--header", "A: 1")
	before := collectionFileBytes(t)

	stdout, stderr := mustRun(t, exitSuccess, "run", "API/Go",
		"--method", "POST", "--query", "b=2", "--header", "B: 2", "--body", "replaced")
	if stdout != "overridden" {
		t.Errorf("stdout = %q, want the server body", stdout)
	}
	if !strings.Contains(stderr, "POST "+srv.URL+"/go?a=1&b=2 -> 200") {
		t.Errorf("stderr = %q, want the status line with the final URL", stderr)
	}

	got := <-rec
	if got.method != http.MethodPost {
		t.Errorf("server saw method %q, want POST", got.method)
	}
	if got.query != "a=1&b=2" {
		t.Errorf("server saw query %q, want saved a=1 before override b=2", got.query)
	}
	if got.header.Get("A") != "1" || got.header.Get("B") != "2" {
		t.Errorf("server saw headers %v, want A: 1 and B: 2", got.header)
	}
	if string(got.body) != "replaced" {
		t.Errorf("server saw body %q, want the override body", got.body)
	}

	if after := collectionFileBytes(t); !bytes.Equal(before, after) {
		t.Error("run rewrote the collection file")
	}
}

func TestRunUnresolvedPlaceholder(t *testing.T) {
	t.Chdir(t.TempDir())

	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer srv.Close()

	mustRun(t, exitSuccess, "request", "create", "API/P", "--method", "GET", "--url", "{{base_url}}/x")

	stdout, stderr := mustRun(t, exitUsage, "run", "API/P")
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if !strings.Contains(stderr, "{{base_url}}") {
		t.Errorf("stderr = %q, want it to name {{base_url}}", stderr)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("server hits = %d, want 0 (Prepare must fail before Execute)", n)
	}
}

func TestRunNotFound(t *testing.T) {
	t.Chdir(t.TempDir())

	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")
	mustRun(t, exitSuccess, "folder", "create", "API/Auth")
	mustRun(t, exitSuccess, "request", "create", "API/Auth/Login", "--method", "GET", "--url", "http://example.com/")

	_, stderr := mustRun(t, exitUsage, "run", "Nope/X")
	if !strings.Contains(stderr, "not found") {
		t.Errorf("missing collection: stderr = %q, want not found", stderr)
	}
	_, stderr = mustRun(t, exitUsage, "run", "API")
	if !strings.Contains(stderr, "names the collection, not a request") {
		t.Errorf("collection path: stderr = %q", stderr)
	}
	_, stderr = mustRun(t, exitUsage, "run", "API/Auth")
	if !strings.Contains(stderr, "is a folder, not a request") {
		t.Errorf("folder path: stderr = %q", stderr)
	}
	_, stderr = mustRun(t, exitUsage, "run")
	if !strings.Contains(stderr, "exactly one PATH") {
		t.Errorf("no positional: stderr = %q", stderr)
	}
	mustRun(t, exitUsage, "run", "API/Auth/Login", "--bogus")
	mustRun(t, exitUsage, "run", "API/Auth/Login", "extra")
}

func TestRunFailFlag(t *testing.T) {
	t.Chdir(t.TempDir())

	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	}))
	defer srv.Close()

	mustRun(t, exitSuccess, "request", "create", "API/R", "--method", "GET", "--url", srv.URL)

	stdout, stderr := mustRun(t, exitSuccess, "run", "API/R")
	if stdout != "boom" {
		t.Errorf("without --fail: stdout = %q, want the 500 body", stdout)
	}
	if !strings.Contains(stderr, "500") {
		t.Errorf("without --fail: stderr = %q, want the status line", stderr)
	}

	stdout, stderr = mustRun(t, exitHTTPFail, "run", "API/R", "--fail")
	if stdout != "boom" {
		t.Errorf("with --fail: stdout = %q, want the 500 body", stdout)
	}
	if !strings.Contains(stderr, "500") {
		t.Errorf("with --fail: stderr = %q, want the status line", stderr)
	}
}

func TestRunScriptBindings(t *testing.T) {
	t.Chdir(t.TempDir())

	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")

	rec := make(chan recordedRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec <- recordedRequest{method: r.Method, path: r.URL.Path, query: r.URL.RawQuery, header: r.Header.Clone(), body: body}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	mustRun(t, exitSuccess, "request", "create", "API/Session",
		"--method", "GET", "--url", srv.URL+"/session/{{who}}", "--header", "X-Who: {{who}}")
	setScripts(t, "API/Session", &model.Scripts{
		PreRequest:   []model.Script{{ID: "pre", Source: `pm.variables.set("who", "script");`, Enabled: true}},
		PostResponse: []model.Script{{ID: "post", Source: `console.log("token-check " + JSON.stringify(pm.response.json()));`, Enabled: true}},
	})
	before := collectionFileBytes(t)

	stdout, stderr := mustRun(t, exitSuccess, "run", "API/Session")
	if stdout != `{"ok":true}` {
		t.Errorf("stdout = %q, want the server body", stdout)
	}
	if !strings.Contains(stderr, `post: token-check {"ok":true}`) {
		t.Errorf("stderr = %q, want the post script's pm.response.json() log", stderr)
	}

	got := <-rec
	if got.path != "/session/script" {
		t.Errorf("server saw path %q, want the pre-script variable resolved into the URL", got.path)
	}
	if got.header.Get("X-Who") != "script" {
		t.Errorf("server saw X-Who %q, want the resolved value", got.header.Get("X-Who"))
	}

	// Variable mutation and response access change nothing on disk.
	if after := collectionFileBytes(t); !bytes.Equal(before, after) {
		t.Error("run rewrote the collection file")
	}
}

func TestRunAssertions(t *testing.T) {
	t.Chdir(t.TempDir())

	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/missing" {
			w.WriteHeader(http.StatusNotFound)
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	mustRun(t, exitSuccess, "request", "create", "API/Checked",
		"--method", "GET", "--url", srv.URL+"/checked")

	// Passing post tests: one PASS line each, exit 0, body on stdout.
	setScripts(t, "API/Checked", &model.Scripts{
		PostResponse: []model.Script{{ID: "post", Source: `
			pm.test("ok status", function () { pm.response.to.have.status(200); });
			pm.test("body ok", function () { pm.expect(pm.response.json().ok).to.be.true; });
		`, Enabled: true}},
	})
	stdout, stderr := mustRun(t, exitSuccess, "run", "API/Checked")
	if stdout != `{"ok":true}` {
		t.Errorf("stdout = %q, want the server body", stdout)
	}
	if !strings.Contains(stderr, "post: PASS ok status") || !strings.Contains(stderr, "post: PASS body ok") {
		t.Errorf("stderr = %q, want a PASS line per passing test", stderr)
	}
	var jsonStdout, jsonStderr bytes.Buffer
	if code := Run(context.Background(), []string{"run", "API/Checked", "--output-format", "json"}, &jsonStdout, &jsonStderr); code != exitSuccess {
		t.Fatalf("JSON run exit = %d, stderr=%q", code, jsonStderr.String())
	}
	var envelope struct {
		Body  string           `json:"body"`
		Logs  []string         `json:"logs"`
		Tests []map[string]any `json:"tests"`
	}
	if err := json.Unmarshal(jsonStdout.Bytes(), &envelope); err != nil {
		t.Fatalf("JSON run stdout = %q: %v", jsonStdout.String(), err)
	}
	if envelope.Body != `{"ok":true}` || len(envelope.Logs) != 0 || len(envelope.Tests) != 2 || strings.Contains(jsonStdout.String(), "post: PASS") {
		t.Fatalf("JSON run envelope = %#v, stdout=%q", envelope, jsonStdout.String())
	}

	// A failed test does not stop later tests in the entry; exit 6.
	setScripts(t, "API/Checked", &model.Scripts{
		PostResponse: []model.Script{{ID: "post", Source: `
			pm.test("wants404", function () { pm.response.to.have.status(404); });
			pm.test("after", function () { pm.expect(1).to.equal(1); });
		`, Enabled: true}},
	})
	stdout, stderr = mustRun(t, exitAssertions, "run", "API/Checked")
	if stdout != `{"ok":true}` {
		t.Errorf("stdout = %q, want the server body", stdout)
	}
	if !strings.Contains(stderr, "post: FAIL wants404: expected response to have status code 404 but got 200") {
		t.Errorf("stderr = %q, want the single-line FAIL diagnostic", stderr)
	}
	if !strings.Contains(stderr, "post: PASS after") {
		t.Errorf("stderr = %q, want the later test to still run", stderr)
	}

	// A failed pre test does not cancel the send; exit 6 with the body.
	hits.Store(0)
	setScripts(t, "API/Checked", &model.Scripts{
		PreRequest: []model.Script{{ID: "pre", Source: `pm.test("nope", function () { pm.expect(1).to.equal(2); });`, Enabled: true}},
	})
	stdout, stderr = mustRun(t, exitAssertions, "run", "API/Checked")
	if stdout != `{"ok":true}` {
		t.Errorf("stdout = %q, want the server body (a failed pre test must not cancel the send)", stdout)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("server hits = %d, want 1", n)
	}
	if !strings.Contains(stderr, "pre: FAIL nope: expected 1 to equal 2") {
		t.Errorf("stderr = %q, want the pre-phase FAIL line", stderr)
	}

	// A post-script runtime error still exits 5 (5 beats 6).
	setScripts(t, "API/Checked", &model.Scripts{
		PreRequest:   []model.Script{{ID: "pre", Source: `pm.test("nope", function () { pm.expect(1).to.equal(2); });`, Enabled: true}},
		PostResponse: []model.Script{{ID: "post", Source: `throw new Error("boom")`, Enabled: true}},
	})
	_, stderr = mustRun(t, exitScript, "run", "API/Checked")
	if !strings.Contains(stderr, "pre: FAIL nope") {
		t.Errorf("stderr = %q, want the failed pre test on the way to exit 5", stderr)
	}

	// A failed assertion beats --fail's HTTP exit 4 (6 beats 4).
	setScripts(t, "API/Checked", &model.Scripts{
		PostResponse: []model.Script{{ID: "post", Source: `pm.test("wants200", function () { pm.response.to.have.status(200); });`, Enabled: true}},
	})
	stdout, stderr = mustRun(t, exitAssertions, "run", "API/Checked",
		"--url", srv.URL+"/missing", "--fail")
	if stdout != `{"ok":true}` {
		t.Errorf("stdout = %q, want the 404 body", stdout)
	}
	if !strings.Contains(stderr, "post: FAIL wants200: expected response to have status code 200 but got 404") {
		t.Errorf("stderr = %q, want the failed status assertion", stderr)
	}
}
