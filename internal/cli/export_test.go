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
	"testing"

	"github.com/SwastikGorai/req/internal/model"
	"github.com/SwastikGorai/req/internal/store"
)

func TestCurlExportCLI(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")
	mustRun(t, exitSuccess, "request", "create", "API/Ping", "--method", "GET", "--url", "https://example.test/{{path}}", "--header", "Authorization: Bearer {{token}}")
	ws, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.SaveEnvironment(context.Background(), model.Environment{
		SchemaVersion: model.SchemaVersion,
		Name:          "dev",
		Variables:     map[string]any{"path": "users", "token": "secret"},
	}, ""); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"export", "curl", "API/Ping"}, &stdout, &stderr); code != exitSuccess {
		t.Fatalf("default export exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "{{path}}") || !strings.Contains(stdout.String(), "{{token}}") {
		t.Fatalf("default export = %q", stdout.String())
	}
	stdout, stderr = bytes.Buffer{}, bytes.Buffer{}
	if code := Run(context.Background(), []string{"export", "curl", "API/Ping", "--resolve", "--env", "dev"}, &stdout, &stderr); code != exitSuccess {
		t.Fatalf("resolved export exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "{{") || !strings.Contains(stdout.String(), "secret") {
		t.Fatalf("resolved export = %q", stdout.String())
	}
	stdout, stderr = bytes.Buffer{}, bytes.Buffer{}
	if code := Run(context.Background(), []string{"export", "curl", "API/Ping", "--resolve"}, &stdout, &stderr); code != exitUsage || stdout.Len() != 0 || !strings.Contains(stderr.String(), "may expose secrets") {
		t.Fatalf("resolve validation exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCurlExportLocalEcho(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Method != http.MethodPost || r.URL.Query().Get("q") != "a b" || r.Header.Get("X-Echo") != "one 'two'" || string(body) != "name=Ada" {
			t.Errorf("echo saw method=%s query=%q header=%q body=%q", r.Method, r.URL.Query().Get("q"), r.Header.Get("X-Echo"), body)
		}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()
	mustRun(t, exitSuccess, "request", "create", "API/Echo", "--method", "POST", "--url", srv.URL+"/echo", "--query", "q=a b", "--header", "X-Echo: one 'two'", "--body", "name=Ada")

	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"export", "curl", "API/Echo"}, &stdout, &stderr); code != exitSuccess {
		t.Fatalf("export exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	source := filepath.Join(t.TempDir(), "echo.curl")
	if err := os.WriteFile(source, []byte(stdout.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr = bytes.Buffer{}, bytes.Buffer{}
	if code := Run(context.Background(), []string{"import", "curl", "--file", source, "--save-as", "API/Roundtrip"}, &stdout, &stderr); code != exitSuccess {
		t.Fatalf("import exported command exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout, stderr = bytes.Buffer{}, bytes.Buffer{}
	if code := Run(context.Background(), []string{"run", "API/Roundtrip"}, &stdout, &stderr); code != exitSuccess || stdout.String() != "ok" {
		t.Fatalf("round-trip run exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCurlExportInheritedScriptsWarnAndStrict(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")
	mustRun(t, exitSuccess, "request", "create", "API/Ping", "--method", "GET", "--url", "https://example.test")
	ws, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	rp, err := ws.ResolvePath(context.Background(), "API")
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.UpdateScripts(context.Background(), "API", &model.Scripts{PreRequest: []model.Script{{ID: "pre", Source: "throw new Error('must not execute')", Enabled: true}}}, rp.Rev); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"export", "curl", "API/Ping"}, &stdout, &stderr); code != exitSuccess || stdout.Len() == 0 || !strings.Contains(stderr.String(), "scripts-omitted") || strings.Contains(stderr.String(), "must not execute") {
		t.Fatalf("lenient script export exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout, stderr = bytes.Buffer{}, bytes.Buffer{}
	if code := Run(context.Background(), []string{"export", "curl", "API/Ping", "--strict"}, &stdout, &stderr); code == exitSuccess || stdout.Len() != 0 || !strings.Contains(stderr.String(), "strict export") || !strings.Contains(stderr.String(), "scripts-omitted") {
		t.Fatalf("strict script export exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
