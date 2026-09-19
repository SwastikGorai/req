package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SwastikGorai/req/internal/store"
)

func TestPostmanImportCLI(t *testing.T) {
	root := t.TempDir()
	if code := Run(context.Background(), []string{"--workspace", root, "init"}, &bytes.Buffer{}, &bytes.Buffer{}); code != exitSuccess {
		t.Fatalf("init exit = %d", code)
	}
	source := filepath.Join("..", "importer", "testdata", "synthetic-collection.json")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--workspace", root, "import", "postman", source}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("import exit = %d (stdout=%q stderr=%q)", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "imported collection \"Imported API\"") || !strings.Contains(stderr.String(), "1 folders, 3 requests, 3 scripts") {
		t.Fatalf("summary = %q", stderr.String())
	}
	ws, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := ws.ListCollections(context.Background()); err != nil || len(got) != 1 {
		t.Fatalf("collections = %d, %v", len(got), err)
	}
	stdout, stderr = bytes.Buffer{}, bytes.Buffer{}
	envSource := filepath.Join("..", "importer", "testdata", "synthetic-environment.json")
	code = Run(context.Background(), []string{"--workspace", root, "import", "postman-env", envSource, "--name", "local"}, &stdout, &stderr)
	if code != exitSuccess || !strings.Contains(stderr.String(), "imported environment \"local\"") {
		t.Fatalf("environment import exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestPostmanImportStrictNoWriteCLI(t *testing.T) {
	root := t.TempDir()
	if code := Run(context.Background(), []string{"--workspace", root, "init"}, &bytes.Buffer{}, &bytes.Buffer{}); code != exitSuccess {
		t.Fatalf("init exit = %d", code)
	}
	source := filepath.Join(t.TempDir(), "lossy.json")
	data := []byte(`{"info":{"_postman_id":"strict-cli-1","name":"Strict CLI","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},"item":[{"name":"R","request":{"method":"GET","url":"https://example.test","auth":{"type":"oauth2"}}}]}`)
	if err := os.WriteFile(source, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--workspace", root, "import", "postman", source, "--strict"}, &stdout, &stderr)
	if code != exitUsage || !strings.Contains(stderr.String(), "strict import") || !strings.Contains(stderr.String(), "unsupported-auth") {
		t.Fatalf("strict exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	ws, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := ws.ListCollections(context.Background()); err != nil || len(got) != 0 {
		t.Fatalf("strict import wrote %d collections (%v)", len(got), err)
	}
}

func TestPostmanUnsupportedAuthBlocked(t *testing.T) {
	root := t.TempDir()
	if code := Run(context.Background(), []string{"--workspace", root, "init"}, &bytes.Buffer{}, &bytes.Buffer{}); code != exitSuccess {
		t.Fatalf("init exit = %d", code)
	}
	source := filepath.Join(t.TempDir(), "blocked.json")
	data := []byte(`{"info":{"_postman_id":"blocked-cli-1","name":"Blocked","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},"item":[{"name":"R","request":{"method":"GET","url":"https://example.test","auth":{"type":"oauth2"}}}]}`)
	if err := os.WriteFile(source, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := Run(context.Background(), []string{"--workspace", root, "import", "postman", source}, &bytes.Buffer{}, &bytes.Buffer{}); code != exitSuccess {
		t.Fatalf("import exit = %d", code)
	}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--workspace", root, "run", "Blocked/R"}, &stdout, &stderr)
	if code != exitUsage || !strings.Contains(stderr.String(), "blocked") || stdout.Len() != 0 {
		t.Fatalf("run exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}
