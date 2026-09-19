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

	"req/internal/execution"
	"req/internal/store"
)

func TestJSONSingleEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Authorization", "response-secret")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"send", "GET", srv.URL, "--output-format", "json", "--verbose"}, &stdout, &stderr); code != exitSuccess {
		t.Fatalf("exit = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON envelope: %v (%q)", err, stdout.String())
	}
	if envelope["version"] != float64(1) || envelope["body"] != `{"ok":true}` {
		t.Fatalf("envelope = %#v", envelope)
	}
	if strings.Contains(stdout.String(), "response-secret") || !strings.Contains(stderr.String(), "Authorization: [REDACTED]") {
		t.Fatalf("secret leaked: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "-> 200") || strings.Contains(stdout.String(), "\nreq:") {
		t.Fatalf("diagnostic reached stdout: %q", stdout.String())
	}
}

func TestRawDownloadStreaming(t *testing.T) {
	want := bytes.Repeat([]byte("x"), execution.MaxScriptBodyBytes+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(want)
	}))
	defer srv.Close()
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"send", "GET", srv.URL, "--raw"}, &stdout, &stderr); code != exitSuccess {
		t.Fatalf("exit = %d, stderr=%q", code, stderr.String())
	}
	if !bytes.Equal(stdout.Bytes(), want) {
		t.Fatalf("raw length = %d, want %d", stdout.Len(), len(want))
	}
}

func TestTerminalOnlyPrettyJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"a":1}`)
	}))
	defer srv.Close()
	original := stdoutIsTerminal
	t.Cleanup(func() { stdoutIsTerminal = original })
	stdoutIsTerminal = func(io.Writer) bool { return true }
	var terminal, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"send", "GET", srv.URL}, &terminal, &stderr); code != exitSuccess {
		t.Fatalf("terminal exit = %d, stderr=%q", code, stderr.String())
	}
	if terminal.String() != "{\n  \"a\": 1\n}\n" {
		t.Fatalf("terminal body = %q", terminal.String())
	}
	stdoutIsTerminal = func(io.Writer) bool { return false }
	var pipe bytes.Buffer
	if code := Run(context.Background(), []string{"send", "GET", srv.URL}, &pipe, &stderr); code != exitSuccess {
		t.Fatalf("pipe exit = %d, stderr=%q", code, stderr.String())
	}
	if pipe.String() != `{"a":1}` {
		t.Fatalf("pipe body = %q", pipe.String())
	}
}

func TestOutputPathReference(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "download")
	}))
	defer srv.Close()
	path := filepath.Join(t.TempDir(), "response.bin")
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"send", "GET", srv.URL, "--output", path, "--output-format", "json"}, &stdout, &stderr); code != exitSuccess {
		t.Fatalf("exit = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "download" {
		t.Fatalf("output file = %q, err=%v", data, err)
	}
	var envelope map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["body_path"] != path || envelope["body"] != nil {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestHeaderRedaction(t *testing.T) {
	root := t.TempDir()
	if _, _, err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	config := []byte(`{"schema_version":1,"secret_headers":["X-Secret"]}`)
	if err := os.WriteFile(filepath.Join(root, ".req", "config.json"), config, 0o600); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Authorization", "auth-secret")
		w.Header().Set("Set-Cookie", "cookie-secret")
		w.Header().Set("X-Secret", "config-secret")
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()
	var stdout, stderr bytes.Buffer
	args := []string{"--workspace", root, "send", "GET", srv.URL, "--output-format", "json", "--verbose"}
	if code := Run(context.Background(), args, &stdout, &stderr); code != exitSuccess {
		t.Fatalf("exit = %d, stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	for _, secret := range []string{"auth-secret", "cookie-secret", "config-secret"} {
		if strings.Contains(stdout.String(), secret) || strings.Contains(stderr.String(), secret) {
			t.Fatalf("secret %q leaked: stdout=%q stderr=%q", secret, stdout.String(), stderr.String())
		}
	}
}

func TestDirectJSONSendBlocksOnRecoveryError(t *testing.T) {
	root := t.TempDir()
	if _, _, err := store.Init(root); err != nil {
		t.Fatal(err)
	}
	journal := filepath.Join(root, ".req", "recovery", "variables-journal.json")
	if err := os.WriteFile(journal, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, "must-not-send")
	}))
	defer srv.Close()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"send", "GET", srv.URL, "--output-format", "json", "--verbose"}, &stdout, &stderr)
	if code != exitStorage {
		t.Fatalf("exit = %d, want %d (stderr=%q)", code, exitStorage, stderr.String())
	}
	if stdout.Len() != 0 || hits.Load() != 0 || !strings.Contains(stderr.String(), "incomplete variable persistence transaction") {
		t.Fatalf("stdout=%q hits=%d stderr=%q", stdout.String(), hits.Load(), stderr.String())
	}
}
