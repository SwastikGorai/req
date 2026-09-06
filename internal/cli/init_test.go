package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"req/internal/store"
)

func TestInitCreatesWorkspace(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"init"}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".req", "config.json")); err != nil {
		t.Errorf("config.json missing: %v", err)
	}
	for _, sub := range []string{"collections", "environments", "recovery"} {
		if _, err := os.Stat(filepath.Join(dir, ".req", sub)); err != nil {
			t.Errorf("%s missing: %v", sub, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".req", ".gitignore")); err != nil {
		t.Errorf(".gitignore missing: %v", err)
	}
	if stdout.Len() == 0 {
		t.Error("init printed nothing to stdout")
	}
}

func TestInitIdempotentViaCLI(t *testing.T) {
	dir := t.TempDir()

	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"--workspace", dir, "init"}, &stdout, &stderr); code != exitSuccess {
		t.Fatalf("first init: exit = %d (stderr: %s)", code, stderr.String())
	}
	configPath := filepath.Join(dir, ".req", "config.json")
	custom := "{\n  \"schema_version\": 1,\n  \"note\": \"keep me\"\n}\n"
	if err := os.WriteFile(configPath, []byte(custom), 0o644); err != nil {
		t.Fatalf("editing config: %v", err)
	}

	stdout, stderr = bytes.Buffer{}, bytes.Buffer{}
	if code := Run(context.Background(), []string{"--workspace", dir, "init"}, &stdout, &stderr); code != exitSuccess {
		t.Fatalf("second init: exit = %d (stderr: %s)", code, stderr.String())
	}
	got, _ := os.ReadFile(configPath)
	if string(got) != custom {
		t.Errorf("second init overwrote config.json:\n%s", got)
	}
	if bytes.Contains(stdout.Bytes(), []byte("initialized req workspace")) {
		t.Errorf("second init reported creation: %s", stdout.String())
	}
}

func TestInitWorkspaceEqualsForm(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--workspace=" + dir, "init"}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if _, err := store.Open(dir); err != nil {
		t.Errorf("workspace not created: %v", err)
	}
}

func TestInitTrailingWorkspaceFlag(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"init", "--workspace", dir}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if _, err := store.Open(dir); err != nil {
		t.Errorf("workspace not created: %v", err)
	}
}

func TestInitRejectsArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"init", "--bogus"}, &stdout, &stderr); code != exitUsage {
		t.Errorf("exit = %d, want 2", code)
	}
	if code := Run(context.Background(), []string{"--workspace"}, &stdout, &stderr); code != exitUsage {
		t.Errorf("dangling --workspace: exit = %d, want 2", code)
	}
	if code := Run(context.Background(), []string{"init", "--workspace"}, &stdout, &stderr); code != exitUsage {
		t.Errorf("dangling trailing --workspace: exit = %d, want 2", code)
	}
	if code := Run(context.Background(), []string{"--workspace", t.TempDir(), "init", "--workspace", t.TempDir()}, &stdout, &stderr); code != exitUsage {
		t.Errorf("double --workspace: exit = %d, want 2", code)
	}
}
