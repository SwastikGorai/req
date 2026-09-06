package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCoreGETBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("server saw method %q, want GET", r.Method)
		}
		_, _ = io.WriteString(w, "hello")
	}))
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"send", "GET", srv.URL}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if stdout.String() != "hello" {
		t.Errorf("stdout = %q, want %q", stdout.String(), "hello")
	}
}

func TestCoreTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // the listener is shut down, so the port refuses connections

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"send", "GET", srv.URL}, &stdout, &stderr)
	if code != exitTransport {
		t.Fatalf("exit = %d, want 3", code)
	}
	if stderr.Len() == 0 {
		t.Error("stderr is empty, want a diagnostic")
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
}

func TestCoreNoWorkspace(t *testing.T) {
	t.Chdir(t.TempDir()) // no .req here or in any ancestor

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "no workspace needed")
	}))
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"send", "GET", srv.URL}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("exit = %d, want 0 without a workspace (stderr: %s)", code, stderr.String())
	}
	if stdout.String() != "no workspace needed" {
		t.Errorf("stdout = %q, want %q", stdout.String(), "no workspace needed")
	}
}
