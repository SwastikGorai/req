package scripting

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"req/internal/variables"
)

func TestPromiseAuthBeforeMain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_, _ = io.WriteString(w, `{"token":"t-1"}`)
		case "/main":
			if got := r.Header.Get("X-Token"); got != "t-1" {
				t.Errorf("main X-Token = %q, want t-1", got)
			}
			_, _ = io.WriteString(w, "main")
		}
	}))
	defer srv.Close()

	scope := &variables.Scope{Local: map[string]any{}}
	e := NewEngine(scope)
	defer e.Close()
	e.SetPreRequest(&ExecRequest{
		Method:  http.MethodGet,
		URL:     srv.URL + "/main",
		Headers: [][2]string{{"X-Token", "{{token}}"}},
	})

	rep, err := e.Run(context.Background(), Source{Name: "promise.js", Code: `
		pm.sendRequest("` + srv.URL + `/token").then(function (response) {
			pm.variables.set("token", response.json().token);
		});
	`})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.Skipped {
		t.Fatal("Promise run was unexpectedly skipped")
	}
	if got := scope.Local["token"]; got != "t-1" {
		t.Fatalf("token = %v, want t-1 before the main request is built", got)
	}
}

func TestHandledPromiseRejectionDoesNotFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	e := NewEngine(nil)
	defer e.Close()
	rep, err := e.Run(context.Background(), Source{Name: "handled.js", Code: `
		pm.sendRequest("` + url + `").catch(function (err) {
			console.log("handled:" + String(err));
		});
	`})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rep.Logs) != 1 || !strings.HasPrefix(rep.Logs[0], "handled:") {
		t.Fatalf("Logs = %v, want one handled rejection log", rep.Logs)
	}
}

func TestUnhandledRejectionFails(t *testing.T) {
	e := NewEngine(nil)
	defer e.Close()

	_, err := e.Run(context.Background(), Source{Name: "unhandled.js", Code: `Promise.reject("boom");`})
	if err == nil || !strings.Contains(err.Error(), "unhandled Promise rejection") || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Run error = %v, want an unhandled Promise rejection mentioning boom", err)
	}
}

func TestPromiseResponseAndRejectionChain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Answer", "yes")
		_, _ = io.WriteString(w, `{"value":42}`)
	}))
	defer srv.Close()

	e := NewEngine(nil)
	defer e.Close()
	rep, err := e.Run(context.Background(), Source{Name: "chain.js", Code: `
		pm.sendRequest("` + srv.URL + `")
			.then(function (response) {
				console.log(JSON.stringify({code: response.code, value: response.json().value, answer: response.headers.get("X-Answer")}));
				return Promise.reject("chain boom");
			})
			.catch(function (err) { console.log("caught:" + String(err)); });
	`})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rep.Logs) != 2 || !strings.Contains(rep.Logs[0], `"code":200`) || rep.Logs[1] != "caught:chain boom" {
		t.Fatalf("Logs = %v, want response and handled chain rejection", rep.Logs)
	}
}

func TestAsyncIIFEWaitsForPromise(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	e := NewEngine(nil)
	defer e.Close()
	rep, err := e.Run(context.Background(), Source{Name: "async-iife.js", Code: `
		(async function () {
			var response = await pm.sendRequest("` + srv.URL + `");
			console.log(response.text());
		})();
	`})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rep.Logs) != 1 || rep.Logs[0] != "ok" {
		t.Fatalf("Logs = %v, want [ok]", rep.Logs)
	}
}

func TestPromiseResponseBodyLimitRejects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), maxAuxiliaryBodyBytes+1))
	}))
	defer srv.Close()

	e := NewEngine(nil)
	defer e.Close()
	rep, err := e.Run(context.Background(), Source{Name: "body-limit.js", Code: `
		pm.sendRequest("` + srv.URL + `").catch(function (err) { console.log(String(err)); });
	`})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rep.Logs) != 1 || !strings.Contains(rep.Logs[0], "10 MiB limit") {
		t.Fatalf("Logs = %v, want handled 10 MiB response-limit error", rep.Logs)
	}
}

func TestAsyncCancelDisposesPromiseResult(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	var late atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		late.Add(1)
		close(canceled)
	}))
	defer srv.Close()

	e := NewEngine(nil)
	defer e.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := e.Run(ctx, Source{Name: "cancel.js", Code: `
			pm.sendRequest("` + srv.URL + `").then(function () { console.log("late"); });
		`})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("auxiliary request never started")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("pending auxiliary request was not canceled")
	}
	if got := late.Load(); got != 1 {
		t.Fatalf("server cancellation count = %d, want 1", got)
	}
}

func TestSkipWithPendingPromiseCancelsHTTP(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(canceled)
	}))
	defer srv.Close()

	e := NewEngine(nil)
	defer e.Close()
	rep, err := e.Run(context.Background(), Source{Name: "skip.js", Code: `
		pm.sendRequest("` + srv.URL + `").then(function () { throw new Error("late"); });
		pm.execution.skipRequest();
	`})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !rep.Skipped {
		t.Fatal("Report.Skipped = false, want true")
	}
	select {
	case <-started:
	case <-canceled:
		return
	case <-time.After(100 * time.Millisecond):
		// skipRequest may cancel before the worker reaches the loopback server;
		// in that case the worker still must finish promptly.
		e.workers.Wait()
		return
	}
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("pending auxiliary request was not canceled")
	}
}

func TestAuxiliaryDeadlineCancelsHTTP(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(canceled)
	}))
	defer srv.Close()

	e := NewEngine(nil)
	defer e.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := e.Run(ctx, Source{Name: "deadline.js", Code: `pm.sendRequest("` + srv.URL + `");`})
	if err == nil || (!errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "deadline")) {
		t.Fatalf("Run error = %v, want deadline", err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("auxiliary request never started")
	}
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("deadline did not cancel auxiliary request")
	}
}

func TestUnsupportedTimerAndModuleFailLoudly(t *testing.T) {
	for _, code := range []string{"setTimeout(function () {}, 1);", `require("x");`} {
		e := NewEngine(nil)
		_, err := e.Run(context.Background(), Source{Name: "unsupported.js", Code: code})
		_ = e.Close()
		name := strings.Split(strings.TrimSpace(code), "(")[0]
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("Run(%q) error = %v, want unsupported API failure", code, err)
		}
	}
}
