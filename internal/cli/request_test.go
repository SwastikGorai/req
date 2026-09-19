package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SwastikGorai/req/internal/model"
)

func TestRequestShowList(t *testing.T) {
	t.Chdir(t.TempDir())

	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")
	mustRun(t, exitSuccess, "folder", "create", "API/Auth")
	mustRun(t, exitSuccess, "request", "create", "API/Auth/Login",
		"--method", "POST", "--url", "https://x/login",
		"--header", "X-Test: 1", "--query", "u=1")
	mustRun(t, exitSuccess, "request", "create", "API/Ping",
		"--method", "GET", "--url", "https://x/ping")

	stdout, _ := mustRun(t, exitSuccess, "request", "show", "API/Auth/Login")
	if !strings.HasPrefix(stdout, "{\n  ") {
		t.Errorf("show output is not indented JSON: %q", stdout)
	}
	var req model.Request
	if err := json.Unmarshal([]byte(stdout), &req); err != nil {
		t.Fatalf("show output is not JSON: %v\n%s", err, stdout)
	}
	if req.Method != "POST" || req.URL != "https://x/login" {
		t.Errorf("show = %s %q, want POST https://x/login", req.Method, req.URL)
	}
	if len(req.Headers) != 1 || req.Headers[0].Key != "X-Test" || req.Headers[0].Value != "1" || !req.Headers[0].Enabled {
		t.Errorf("show headers = %+v, want X-Test: 1 enabled", req.Headers)
	}
	if len(req.Query) != 1 || req.Query[0].Key != "u" || req.Query[0].Value != "1" || !req.Query[0].Enabled {
		t.Errorf("show query = %+v, want u=1 enabled", req.Query)
	}

	stdout, _ = mustRun(t, exitSuccess, "request", "list", "API/Auth")
	if stdout != "Login\tPOST\thttps://x/login\n" {
		t.Errorf("list API/Auth = %q, want the Login line", stdout)
	}
	stdout, _ = mustRun(t, exitSuccess, "request", "list", "API")
	if stdout != "Ping\tGET\thttps://x/ping\n" {
		t.Errorf("list API = %q, want only Ping (folders are skipped)", stdout)
	}

	_, stderr := mustRun(t, exitUsage, "request", "list", "API/Ping")
	if !strings.Contains(stderr, "is a request, not a folder") {
		t.Errorf("list on a request: stderr = %q", stderr)
	}
	_, stderr = mustRun(t, exitUsage, "request", "show", "API/Auth")
	if !strings.Contains(stderr, "is a folder, not a request") {
		t.Errorf("show on a folder: stderr = %q", stderr)
	}
	_, stderr = mustRun(t, exitUsage, "request", "show", "API")
	if !strings.Contains(stderr, "names the collection, not a request") {
		t.Errorf("show on the collection root: stderr = %q", stderr)
	}
}

func TestRequestCreateValidation(t *testing.T) {
	t.Chdir(t.TempDir())

	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")

	_, stderr := mustRun(t, exitUsage, "request", "create", "API/NoMethod", "--url", "https://x/")
	if !strings.Contains(stderr, "--method") {
		t.Errorf("missing --method: stderr = %q, want it to name the flag", stderr)
	}
	_, stderr = mustRun(t, exitUsage, "request", "create", "API/NoURL", "--method", "GET")
	if !strings.Contains(stderr, "--url") {
		t.Errorf("missing --url: stderr = %q, want it to name the flag", stderr)
	}
	mustRun(t, exitUsage, "request", "create", "API/Bad", "--method", "GET", "--url", "https://x/", "--body", "x", "--json", "{}")
	mustRun(t, exitUsage, "request", "create", "API/Bad", "--method", "GE@T", "--url", "https://x/")
	mustRun(t, exitUsage, "request", "create", "API/Bad", "--method", "GET", "--url", "https://x/", "--json", "{nope")
	mustRun(t, exitUsage, "request", "create", "API/Bad", "--method", "GET", "--url", "https://x/", "--header", "NoColon")
	mustRun(t, exitUsage, "request", "create", "API/Bad", "--method", "GET", "--url", "https://x/", "--query", "noseparator")
	mustRun(t, exitUsage, "request", "create", "A", "B", "--method", "GET", "--url", "https://x/")
	mustRun(t, exitUsage, "request", "create", "API/Bad", "--method", "GET", "--url", "https://x/", "--bogus")

	// A --json body containing a {{reference}} is stored verbatim.
	mustRun(t, exitSuccess, "request", "create", "API/Var",
		"--method", "POST", "--url", "https://x/", "--json", `{"token": "{{api_token}}"}`)
	stdout, _ := mustRun(t, exitSuccess, "request", "show", "API/Var")
	if !strings.Contains(stdout, "{{api_token}}") {
		t.Errorf("show = %q, want the verbatim {{api_token}} reference", stdout)
	}
}
