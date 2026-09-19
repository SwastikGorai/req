package cli

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SwastikGorai/req/internal/model"
	"github.com/SwastikGorai/req/internal/store"
)

func persistWorkspace(t *testing.T, url string, header bool) {
	t.Helper()
	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")
	mustRun(t, exitSuccess, "env", "create", "local")
	args := []string{"request", "create", "API/Token", "--method", "GET", "--url", url}
	if header {
		args = append(args, "--header", "Authorization: Bearer {{token}}", "--header", "X-Collection: {{collection_token}}")
	}
	mustRun(t, exitSuccess, args...)
}

func persistedEnvironment(t *testing.T) model.Environment {
	t.Helper()
	ws, err := store.Open("")
	if err != nil {
		t.Fatal(err)
	}
	e, _, err := ws.LoadEnvironment(context.Background(), "local")
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func persistedCollection(t *testing.T) model.Collection {
	t.Helper()
	ws, err := store.Open("")
	if err != nil {
		t.Fatal(err)
	}
	rp, err := ws.ResolvePath(context.Background(), "API")
	if err != nil {
		t.Fatal(err)
	}
	return rp.Collection
}

func TestPersistTokenAcrossProcesses(t *testing.T) {
	t.Chdir(t.TempDir())
	var authorization []string
	var collectionHeaders []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = append(authorization, r.Header.Get("Authorization"))
		collectionHeaders = append(collectionHeaders, r.Header.Get("X-Collection"))
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()
	persistWorkspace(t, srv.URL, true)
	setScripts(t, "API/Token", &model.Scripts{PreRequest: []model.Script{{ID: "set", Source: `pm.environment.set("token", "fresh"); pm.collectionVariables.set("collection_token", "collection");`, Enabled: true}}})

	mustRun(t, exitSuccess, "run", "API/Token", "--env", "local", "--persist-vars")
	setScripts(t, "API/Token", nil)
	mustRun(t, exitSuccess, "run", "API/Token", "--env", "local")
	if len(authorization) != 2 || authorization[1] != "Bearer fresh" {
		t.Fatalf("authorization = %#v, want the persisted token on the second invocation", authorization)
	}
	if len(collectionHeaders) != 2 || collectionHeaders[1] != "collection" {
		t.Fatalf("collection headers = %#v, want the persisted collection variable", collectionHeaders)
	}
	if got := persistedEnvironment(t).Variables["token"]; got != "fresh" {
		t.Fatalf("persisted token = %#v, want fresh", got)
	}
}

func TestNoPersistByDefault(t *testing.T) {
	t.Chdir(t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()
	persistWorkspace(t, srv.URL, true)
	setScripts(t, "API/Token", &model.Scripts{PreRequest: []model.Script{{ID: "set", Source: `pm.environment.set("token", "fresh"); pm.collectionVariables.set("collection_token", "collection");`, Enabled: true}}})
	mustRun(t, exitSuccess, "run", "API/Token", "--env", "local")
	setScripts(t, "API/Token", nil)
	_, stderr := mustRun(t, exitUsage, "run", "API/Token", "--env", "local")
	if !strings.Contains(stderr, "{{token}}") {
		t.Fatalf("stderr = %q, want an unresolved token", stderr)
	}
	if _, ok := persistedEnvironment(t).Variables["token"]; ok {
		t.Fatal("token was persisted without --persist-vars")
	}
	if _, ok := persistedCollection(t).Variables["collection_token"]; ok {
		t.Fatal("collection token was persisted without --persist-vars")
	}
}

func TestPersistEligibility(t *testing.T) {
	tests := []struct {
		name      string
		expected  int
		pre, post string
		status    int
		persisted bool
		fail      bool
		transport bool
	}{
		{name: "runtime failure", expected: exitScript, pre: `pm.environment.set("token", "runtime"); throw new Error("boom");`},
		{name: "skip", expected: exitSuccess, pre: `pm.environment.set("token", "skip"); pm.execution.skipRequest();`},
		{name: "assertion failure", expected: exitAssertions, post: `pm.environment.set("token", "assertion"); pm.test("fail", function () { pm.expect(1).to.equal(2); });`, persisted: true},
		{name: "http failure", expected: exitHTTPFail, post: `pm.environment.set("token", "http");`, status: http.StatusInternalServerError, persisted: true, fail: true},
		{name: "transport failure", expected: exitTransport, pre: `pm.environment.set("token", "transport");`, transport: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.status != 0 {
					w.WriteHeader(tt.status)
				}
				_, _ = io.WriteString(w, "ok")
			}))
			persistWorkspace(t, srv.URL, false)
			defer srv.Close()
			setScripts(t, "API/Token", &model.Scripts{
				PreRequest:   scriptEntries(tt.pre),
				PostResponse: scriptEntries(tt.post),
			})
			if tt.transport {
				srv.Close()
			}
			args := []string{"run", "API/Token", "--env", "local", "--persist-vars"}
			if tt.fail {
				args = append(args, "--fail")
			}
			mustRun(t, tt.expected, args...)
			_, ok := persistedEnvironment(t).Variables["token"]
			if ok != tt.persisted {
				t.Fatalf("token present = %v, want %v", ok, tt.persisted)
			}
		})
	}
}

func TestPersistVarsIsRunOnly(t *testing.T) {
	parsed, err := parseRunArgs([]string{"API/Token", "--persist-vars"})
	if err != nil || !parsed.persistVars {
		t.Fatalf("parseRunArgs = %#v, err = %v", parsed, err)
	}
	if _, err := parseSendArgs([]string{"GET", "http://example.test", "--persist-vars"}); err == nil {
		t.Fatal("send accepted --persist-vars")
	}
}

func scriptEntries(source string) []model.Script {
	if source == "" {
		return nil
	}
	return []model.Script{{ID: "script", Source: source, Enabled: true}}
}
