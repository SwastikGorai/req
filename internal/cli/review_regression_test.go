package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"req/internal/model"
	"req/internal/store"
)

func TestFolderDeletePreservesChangesDuringConfirmation(t *testing.T) {
	t.Chdir(t.TempDir())
	mustRun(t, 0, "init")
	mustRun(t, 0, "collection", "create", "API")
	mustRun(t, 0, "folder", "create", "API/F")
	mustRun(t, 0, "request", "create", "API/F/Old", "--method", "GET", "--url", "http://localhost")
	oldTerminal, oldConfirm := stdinIsTerminal, confirmDelete
	t.Cleanup(func() { stdinIsTerminal, confirmDelete = oldTerminal, oldConfirm })
	stdinIsTerminal = func() bool { return true }
	w, err := store.Discover("")
	if err != nil {
		t.Fatal(err)
	}
	confirmDelete = func(_ string, _ io.Writer) bool {
		if err := w.CreateRequest(context.Background(), "API/F/New", model.Request{Method: "GET", URL: "http://localhost"}, false); err != nil {
			t.Fatal(err)
		}
		return true
	}
	mustRun(t, 7, "folder", "delete", "API/F")
	for _, path := range []string{"API/F/Old", "API/F/New"} {
		if _, err := w.ResolvePath(context.Background(), path); err != nil {
			t.Fatalf("lost %s: %v", path, err)
		}
	}
}

type cancelResponseWriter struct{ cancel context.CancelFunc }

func (w cancelResponseWriter) Write(b []byte) (int, error) { w.cancel(); return len(b), nil }

func TestCancelDuringResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "prefix")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var diagnostics bytes.Buffer
	code := Run(ctx, []string{"send", "GET", srv.URL}, cancelResponseWriter{cancel}, &diagnostics)
	if code != 130 {
		t.Fatalf("wanted cancellation 130; got %d: %s", code, diagnostics.String())
	}
}

func TestHostOverride(t *testing.T) {
	t.Chdir(t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, r.Host) }))
	defer srv.Close()
	out, _ := mustRun(t, 0, "send", "GET", srv.URL, "-H", "Host: virtual.example")
	if out != "virtual.example" {
		t.Fatalf("server received Host %q", out)
	}
	mustRun(t, 0, "init")
	mustRun(t, 0, "collection", "create", "API")
	mustRun(t, 0, "request", "create", "API/R", "--method", "GET", "--url", srv.URL, "-H", "host: {{host}}")
	out, _ = mustRun(t, 0, "run", "API/R", "--var", "host=saved.example")
	if out != "saved.example" {
		t.Fatalf("saved request sent Host %q", out)
	}
}
