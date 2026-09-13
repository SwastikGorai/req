package scripting

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type delayedTransport func(*http.Request) (*http.Response, error)

func (f delayedTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLateCompletionPreservesCurrentWork(t *testing.T) {
	oldStarted, newStarted := make(chan struct{}), make(chan struct{})
	oldRelease, newRelease := make(chan struct{}), make(chan struct{})
	e := newSpikeEngine()
	e.httpClient = &http.Client{Transport: delayedTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/old" {
			close(oldStarted)
			<-oldRelease
			return nil, errors.New("old request canceled")
		}
		close(newStarted)
		<-newRelease
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})}
	defer e.Close()
	defer close(newRelease)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := e.Run(ctx, Source{Name: "old", Code: `httpGet("http://local/old",function(){}); throw new Error("stop");`}); err == nil {
		t.Fatal("first script should fail")
	}
	<-oldStarted
	done := make(chan runResult, 1)
	go func() {
		report, err := e.Run(ctx, Source{Name: "new", Code: `httpGet("http://local/new",function(){log("finished");});`})
		done <- runResult{report, err}
	}()
	<-newStarted
	close(oldRelease)
	// Give the old completion time to arrive without releasing the current
	// request. Only the old worker can finish at this point.
	select {
	case result := <-done:
		t.Fatalf("second run finished before its request: %+v", result)
	case <-time.After(100 * time.Millisecond):
	}
	// Release through the response gate; leave channel closure to the defer.
	newRelease <- struct{}{}
	result := <-done
	if result.err != nil || len(result.report.Logs) != 1 || result.report.Logs[0] != "finished" {
		t.Fatalf("current callback lost: %+v", result)
	}
}
