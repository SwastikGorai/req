package cli

import (
	"strings"
	"testing"
)

func TestTreeOutput(t *testing.T) {
	t.Chdir(t.TempDir())

	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")
	mustRun(t, exitSuccess, "folder", "create", "API/Auth")
	mustRun(t, exitSuccess, "folder", "create", "API/Auth/Deep", "--parents")
	mustRun(t, exitSuccess, "request", "create", "API/Auth/Deep/Login", "--method", "POST", "--url", "https://x/login")
	mustRun(t, exitSuccess, "request", "create", "API/Ping", "--method", "GET", "--url", "https://x/ping")

	want := "API\nAuth/\n  Deep/\n    Login  POST https://x/login\nPing  GET https://x/ping\n"
	stdout, _ := mustRun(t, exitSuccess, "tree", "API")
	if stdout != want {
		t.Errorf("tree API = %q, want %q", stdout, want)
	}

	want = "Auth\nDeep/\n  Login  POST https://x/login\n"
	stdout, _ = mustRun(t, exitSuccess, "tree", "API/Auth")
	if stdout != want {
		t.Errorf("tree API/Auth = %q, want %q", stdout, want)
	}

	_, stderr := mustRun(t, exitUsage, "tree", "API/Auth/Deep/Login")
	if !strings.Contains(stderr, "is a request") {
		t.Errorf("tree on a request: stderr = %q", stderr)
	}
	mustRun(t, exitUsage, "tree")
	mustRun(t, exitUsage, "tree", "API", "extra")
}
