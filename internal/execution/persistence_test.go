package execution

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"req/internal/model"
)

func TestPersistFailurePrecedence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()
	sp := ScriptPolicy{
		Persist: func(context.Context) error { return errors.New("conflict") },
	}
	post := []model.Script{scriptEntry("post", `pm.test("fail", function () { pm.expect(1).to.equal(2); });`, true)}
	code, _, stderr := runLifecycle(t, context.Background(), model.Request{Method: "GET", URL: srv.URL}, sp, nil, post)
	if code != codeStorage {
		t.Fatalf("exit = %d, want %d (stderr: %s)", code, codeStorage, stderr)
	}
}

func TestPersistCancellationPrecedence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	sp := ScriptPolicy{
		Persist: func(context.Context) error {
			cancel()
			return errors.New("conflict")
		},
	}
	code, _, _ := runLifecycle(t, ctx, model.Request{Method: "GET", URL: srv.URL}, sp, nil, nil)
	if code != codeCanceled {
		t.Fatalf("exit = %d, want %d", code, codeCanceled)
	}
}
