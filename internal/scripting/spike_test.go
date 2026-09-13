package scripting

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dop251/goja"
)

func TestRuntimeVariableRoundTrip(t *testing.T) {
	e := newSpikeEngine()
	defer e.Close()

	rep, err := e.Run(context.Background(), Source{
		Name: "vars.js",
		Code: `
			vars.set("token", "abc123");
			vars.set("count", 42);
			log("stored");
		`,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := e.vars.get("token"); got != "abc123" {
		t.Errorf("vars[token] = %v (%T), want %q", got, got, "abc123")
	}
	if got := e.vars.get("count"); got != int64(42) {
		t.Errorf("vars[count] = %v (%T), want int64(42)", got, got)
	}
	if len(rep.Logs) != 1 || rep.Logs[0] != "stored" {
		t.Errorf("Logs = %v, want [stored]", rep.Logs)
	}
}

func TestRuntimeSourceLocationError(t *testing.T) {
	e := newSpikeEngine()
	defer e.Close()

	_, err := e.Run(context.Background(), Source{
		Name: "spike.js",
		Code: "var a = 1;\nvar b = 2;\nthrow new Error('boom');\n",
	})
	if err == nil {
		t.Fatal("Run succeeded, want the thrown error")
	}
	var exc *goja.Exception
	if !errors.As(err, &exc) {
		t.Fatalf("err = %T (%v), want *goja.Exception", err, err)
	}
	if !strings.Contains(exc.Error(), "spike.js:3") {
		t.Errorf("error %q does not reference source location spike.js:3", exc.Error())
	}
}

func TestRuntimeInterrupt(t *testing.T) {
	e := newSpikeEngine()
	defer e.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := e.Run(ctx, Source{Name: "spin.js", Code: "for(;;){}"})
		done <- err
	}()
	select {
	case err := <-done:
		var interrupted *goja.InterruptedError
		if !errors.Is(err, context.DeadlineExceeded) && !errors.As(err, &interrupted) {
			t.Fatalf("err = %v, want a deadline or interrupt error", err)
		}
	case <-time.After(5 * time.Second):
		// Outer guard: a broken watchdog must fail the test mechanically
		// instead of hanging the process.
		t.Fatal("Run did not return within 5s: watchdog failed to interrupt the infinite loop")
	}
}

func TestRuntimeCallbackAndPromise(t *testing.T) {
	e := newSpikeEngine()
	defer e.Close()

	mux := http.NewServeMux()
	mux.HandleFunc("/a", delayed(20*time.Millisecond, "alpha"))
	mux.HandleFunc("/b", delayed(20*time.Millisecond, "beta"))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	script := fmt.Sprintf(`
		log("script start");
		httpGet(%q, function (err, resp) {
			if (err) { log("callback error: " + err); return; }
			vars.set("callback", resp.status + ":" + resp.body);
		});
		httpGetAsync(%q).then(function (resp) {
			vars.set("promise", resp.status + ":" + resp.body);
			log("promise settled");
		});
		log("script end");
	`, srv.URL+"/a", srv.URL+"/b")

	rep, err := e.Run(context.Background(), Source{Name: "async.js", Code: script})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := e.vars.get("callback"); got != "200:alpha" {
		t.Errorf("callback result = %v, want %q", got, "200:alpha")
	}
	if got := e.vars.get("promise"); got != "200:beta" {
		t.Errorf("promise result = %v, want %q", got, "200:beta")
	}
	want := []string{"script start", "script end", "promise settled"}
	if len(rep.Logs) != len(want) {
		t.Fatalf("Logs = %v, want %v", rep.Logs, want)
	}
	for i := range want {
		if rep.Logs[i] != want[i] {
			t.Errorf("Logs[%d] = %q, want %q (full: %v)", i, rep.Logs[i], want[i], rep.Logs)
		}
	}
}

func TestRuntimeCancellation(t *testing.T) {
	e := newSpikeEngine()
	defer e.Close()

	handlerStarted := make(chan struct{})
	handlerDone := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(handlerStarted)
		defer close(handlerDone)
		<-r.Context().Done() // hold the response until the client goes away
	}))
	defer srv.Close()

	script := fmt.Sprintf(`
		httpGet(%q, function (err, resp) {
			vars.set("settled", err ? "error" : "ok");
		});
	`, srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := e.Run(ctx, Source{Name: "cancel.js", Code: script})
		done <- err
	}()

	select {
	case <-handlerStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("the request never reached the server")
	}
	cancel()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancellation")
	}
	select {
	case <-handlerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("the server handler was never released: pending HTTP work leaked")
	}
	if got := e.vars.get("settled"); got == "ok" {
		t.Error("the callback reported success after cancellation")
	}
}

func delayed(d time.Duration, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(d)
		_, _ = io.WriteString(w, body)
	}
}
