package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"req/internal/store"
)

func postmanScriptFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "importer", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPostmanImportedLogin(t *testing.T) {
	root := t.TempDir()
	if code := Run(context.Background(), []string{"--workspace", root, "init"}, &bytes.Buffer{}, &bytes.Buffer{}); code != exitSuccess {
		t.Fatalf("init exit = %d", code)
	}
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/callback":
			_, _ = io.WriteString(w, `{"token":"callback-token"}`)
		case "/promise":
			_, _ = io.WriteString(w, `{"token":"promise-token"}`)
		case "/profile":
			if r.Header.Get("X-Callback") != "callback-token" || r.Header.Get("X-Promise") != "promise-token" || r.Header.Get("X-Folder") != "folder" {
				http.Error(w, "missing imported script values", http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `{"ok":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	data := postmanScriptFixture(t, "postman-login.json")
	data = bytes.ReplaceAll(data, []byte("https://callback.example.test/token"), []byte(srv.URL+"/callback"))
	data = bytes.ReplaceAll(data, []byte("https://promise.example.test/token"), []byte(srv.URL+"/promise"))
	data = bytes.ReplaceAll(data, []byte("https://api.example.test"), []byte(srv.URL))
	source := filepath.Join(t.TempDir(), "postman-login.json")
	if err := os.WriteFile(source, data, 0o600); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"--workspace", root, "import", "postman", source}, &stdout, &stderr); code != exitSuccess {
		t.Fatalf("import exit = %d (stderr=%q)", code, stderr.String())
	}
	if hits.Load() != 0 {
		t.Fatalf("import executed %d HTTP requests", hits.Load())
	}

	stdout.Reset()
	stderr.Reset()
	code := Run(context.Background(), []string{"--workspace", root, "run", "Imported Login/Auth/Login"}, &stdout, &stderr)
	if code != exitSuccess || stdout.String() != `{"ok":true}` {
		t.Fatalf("run exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if hits.Load() != 3 {
		t.Fatalf("run made %d requests, want two auxiliary requests and the main request", hits.Load())
	}
	status := strings.Index(stderr.String(), "GET "+srv.URL+"/profile -> 200")
	if status < 0 {
		t.Fatalf("stderr=%q, want main status", stderr.String())
	}
	previous := 0
	for _, marker := range []string{"collection-callback", "collection-promise", "folder-pre", "request-pre"} {
		at := strings.Index(stderr.String(), marker)
		if at < previous || at > status {
			t.Fatalf("stderr=%q, marker %q is out of pre-request order", stderr.String(), marker)
		}
		previous = at
	}
	previous = status
	for _, marker := range []string{"collection post", "folder post", "request post"} {
		at := strings.Index(stderr.String(), marker)
		if at < previous {
			t.Fatalf("stderr=%q, marker %q is out of post-response order", stderr.String(), marker)
		}
		previous = at
	}
}

func TestUnsupportedRuntimeAPI(t *testing.T) {
	root := t.TempDir()
	if code := Run(context.Background(), []string{"--workspace", root, "init"}, &bytes.Buffer{}, &bytes.Buffer{}); code != exitSuccess {
		t.Fatalf("init exit = %d", code)
	}
	source := filepath.Join(t.TempDir(), "postman-unsupported-runtime.json")
	if err := os.WriteFile(source, postmanScriptFixture(t, "postman-unsupported-runtime.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"--workspace", root, "import", "postman", source}, &stdout, &stderr); code != exitSuccess {
		t.Fatalf("lenient import exit=%d stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "unsupported-script") {
		t.Fatalf("import stderr=%q, want the obvious unsupported API warning", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code := Run(context.Background(), []string{"--workspace", root, "run", "Unsupported Runtime/Request"}, &stdout, &stderr)
	if code != exitScript || stdout.Len() != 0 {
		t.Fatalf("run exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), `unsupported pm API "pm.globals"`) || !strings.Contains(stderr.String(), "postman:event[0]:1") {
		t.Fatalf("runtime diagnostic=%q, want API and imported source location", stderr.String())
	}
}

func TestPostmanStrictUnsupportedScriptNoWrite(t *testing.T) {
	root := t.TempDir()
	if code := Run(context.Background(), []string{"--workspace", root, "init"}, &bytes.Buffer{}, &bytes.Buffer{}); code != exitSuccess {
		t.Fatalf("init exit = %d", code)
	}
	source := filepath.Join(t.TempDir(), "postman-unsupported-runtime.json")
	if err := os.WriteFile(source, postmanScriptFixture(t, "postman-unsupported-runtime.json"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--workspace", root, "import", "postman", source, "--strict"}, &stdout, &stderr)
	if code != exitUsage || !strings.Contains(stderr.String(), "strict import") || !strings.Contains(stderr.String(), "unsupported-script") {
		t.Fatalf("strict import exit=%d stderr=%q", code, stderr.String())
	}
	ws, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	collections, err := ws.ListCollections(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(collections) != 0 {
		t.Fatalf("strict import wrote %d collections", len(collections))
	}
}
