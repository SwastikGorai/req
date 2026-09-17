package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"req/internal/store"
)

func TestCurlImportCLIAndRedirectPolicy(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			w.Header().Set("Location", "/end")
			w.WriteHeader(http.StatusFound)
			_, _ = io.WriteString(w, "no-follow")
			return
		}
		_, _ = io.WriteString(w, "followed")
	}))
	defer srv.Close()
	source := filepath.Join(t.TempDir(), "request.curl")
	if err := os.WriteFile(source, []byte(fmt.Sprintf("curl %s/start", srv.URL)), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"import", "curl", "--file", source, "--save-as", "API/NoLocation"}, &stdout, &stderr)
	if code != exitSuccess || !strings.Contains(stderr.String(), "imported curl request \"API/NoLocation\"") {
		t.Fatalf("import exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout, stderr = bytes.Buffer{}, bytes.Buffer{}
	code = Run(context.Background(), []string{"run", "API/NoLocation"}, &stdout, &stderr)
	if code != exitSuccess || stdout.String() != "no-follow" {
		t.Fatalf("no-location run exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if err := os.WriteFile(source, []byte(fmt.Sprintf("curl -L %s/start", srv.URL)), 0o600); err != nil {
		t.Fatal(err)
	}
	code = Run(context.Background(), []string{"import", "curl", "--file", source, "--save-as", "API/Location"}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("location import exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout, stderr = bytes.Buffer{}, bytes.Buffer{}
	code = Run(context.Background(), []string{"run", "API/Location"}, &stdout, &stderr)
	if code != exitSuccess || stdout.String() != "followed" {
		t.Fatalf("location run exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestCurlImportStrictNoWriteCLI(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")
	source := filepath.Join(t.TempDir(), "lossy.curl")
	if err := os.WriteFile(source, []byte("curl --data @missing.bin https://example.test"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"import", "curl", "--file", source, "--save-as", "API/Lossy", "--strict"}, &stdout, &stderr)
	if code != exitUsage || !strings.Contains(stderr.String(), "strict import") || !strings.Contains(stderr.String(), "lossy-curl") {
		t.Fatalf("strict import exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	ws, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.ResolvePath(context.Background(), "API/Lossy"); err == nil {
		t.Fatal("strict import wrote a request")
	}
}

func TestCurlImportCLIValidation(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	mustRun(t, exitSuccess, "init")
	mustRun(t, exitSuccess, "collection", "create", "API")
	source := filepath.Join(t.TempDir(), "unsafe.curl")
	if err := os.WriteFile(source, []byte("curl https://example.test | cat"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"import", "curl", "--file", source, "--save-as", "API/Unsafe"}, &stdout, &stderr)
	if code != exitUsage || !strings.Contains(stderr.String(), "active shell construct") {
		t.Fatalf("unsafe import exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	code = Run(context.Background(), []string{"import", "curl", "--file", source}, &stdout, &stderr)
	if code != exitUsage {
		t.Fatalf("missing save-as exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
