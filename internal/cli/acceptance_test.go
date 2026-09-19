package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"req/internal/model"
)

func TestAcceptanceNativeLogin(t *testing.T) {
	t.Chdir(t.TempDir())
	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")
	mustRun(t, exitSuccess, "env", "create", "local")

	var profileAuthorization atomic.Value
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/login":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"token":"native-token"}`)
		case "/profile":
			profileAuthorization.Store(r.Header.Get("Authorization"))
			if r.Header.Get("Authorization") != "Bearer native-token" {
				http.Error(w, "missing persisted token", http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `{"profile":"ok"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	mustRun(t, exitSuccess, "request", "create", "API/Login", "--method", "POST", "--url", srv.URL+"/login")
	mustRun(t, exitSuccess, "request", "create", "API/Profile", "--method", "GET", "--url", srv.URL+"/profile", "--header", "Authorization: Bearer {{token}}")
	setScripts(t, "API/Login", &model.Scripts{PostResponse: []model.Script{{
		ID:      "extract-token",
		Source:  `pm.environment.set("token", pm.response.json().token);`,
		Enabled: true,
	}}})

	// The two run calls are separate CLI invocations. The second one proves
	// that the post-script mutation crossed the separate CLI invocation boundary
	// via storage.
	mustRun(t, exitSuccess, "run", "API/Login", "--env", "local", "--persist-vars")
	setScripts(t, "API/Login", nil)
	stdout, _ := mustRun(t, exitSuccess, "run", "API/Profile", "--env", "local")
	if stdout != `{"profile":"ok"}` {
		t.Fatalf("profile body = %q, want authenticated response", stdout)
	}
	if got := profileAuthorization.Load(); got != "Bearer native-token" {
		t.Fatalf("profile authorization = %v, want persisted token", got)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("server hits = %d, want login and profile", got)
	}
}

func TestAcceptanceImportedScripts(t *testing.T) {
	data := postmanScriptFixture(t, "postman-login.json")
	root := t.TempDir()
	t.Chdir(root)
	mustRun(t, exitSuccess, "init")

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/callback":
			_, _ = io.WriteString(w, `{"token":"callback-token"}`)
		case "/promise":
			_, _ = io.WriteString(w, `{"token":"promise-token"}`)
		case "/profile":
			if r.Header.Get("X-Callback") != "callback-token" ||
				r.Header.Get("X-Promise") != "promise-token" ||
				r.Header.Get("X-Folder") != "folder" {
				http.Error(w, "missing imported script values", http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `{"ok":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	data = bytes.ReplaceAll(data, []byte("https://callback.example.test/token"), []byte(srv.URL+"/callback"))
	data = bytes.ReplaceAll(data, []byte("https://promise.example.test/token"), []byte(srv.URL+"/promise"))
	data = bytes.ReplaceAll(data, []byte("https://api.example.test"), []byte(srv.URL))
	source := filepath.Join(t.TempDir(), "postman-login.json")
	if err := os.WriteFile(source, data, 0o600); err != nil {
		t.Fatal(err)
	}

	mustRun(t, exitSuccess, "import", "postman", source)
	if got := hits.Load(); got != 0 {
		t.Fatalf("import executed %d HTTP requests", got)
	}
	stdout, stderr := mustRun(t, exitSuccess, "run", "Imported Login/Auth/Login")
	if stdout != `{"ok":true}` {
		t.Fatalf("imported run body = %q, want profile response", stdout)
	}
	if got := hits.Load(); got != 3 {
		t.Fatalf("run made %d requests, want callback, Promise and main", got)
	}
	status := strings.Index(stderr, "GET "+srv.URL+"/profile -> 200")
	if status < 0 {
		t.Fatalf("run diagnostics = %q, want main status", stderr)
	}
	previous := 0
	for _, marker := range []string{"collection-callback", "collection-promise", "folder-pre", "request-pre"} {
		at := strings.Index(stderr, marker)
		if at < previous || at > status {
			t.Fatalf("diagnostics = %q, marker %q is out of inherited pre-script order", stderr, marker)
		}
		previous = at
	}
	previous = status
	for _, marker := range []string{"collection post", "folder post", "request post"} {
		at := strings.Index(stderr, marker)
		if at < previous {
			t.Fatalf("diagnostics = %q, marker %q is out of inherited post-script order", stderr, marker)
		}
		previous = at
	}
}

func TestAcceptanceCurl(t *testing.T) {
	t.Chdir(t.TempDir())
	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")

	type echoRequest struct {
		method string
		query  string
		header string
		body   string
	}
	observed := make(chan echoRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		observed <- echoRequest{method: r.Method, query: r.URL.Query().Get("q"), header: r.Header.Get("X-Echo"), body: string(data)}
		_, _ = io.WriteString(w, "round-trip-ok")
	}))
	defer srv.Close()

	mustRun(t, exitSuccess, "request", "create", "API/Source", "--method", "POST", "--url", srv.URL+"/echo", "--query", "q=a b", "--header", "X-Echo: one 'two'", "--body", "name=Ada")
	curl, _ := mustRun(t, exitSuccess, "export", "curl", "API/Source")
	if !strings.Contains(curl, "curl") || !strings.Contains(curl, srv.URL+"/echo") {
		t.Fatalf("exported command = %q, want cURL command for loopback endpoint", curl)
	}
	source := filepath.Join(t.TempDir(), "round-trip.curl")
	if err := os.WriteFile(source, []byte(curl), 0o600); err != nil {
		t.Fatal(err)
	}
	mustRun(t, exitSuccess, "import", "curl", "--file", source, "--save-as", "API/Imported")
	stdout, _ := mustRun(t, exitSuccess, "run", "API/Imported")
	if stdout != "round-trip-ok" {
		t.Fatalf("round-trip body = %q, want server response", stdout)
	}
	got := <-observed
	if got.method != http.MethodPost || got.query != "a b" || got.header != "one 'two'" || got.body != "name=Ada" {
		t.Fatalf("round-trip request method=%q query=%q header=%q body=%q", got.method, got.query, got.header, got.body)
	}
}
