package httpclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendGET(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("server saw method %q, want GET", r.Method)
		}
		w.Header().Set("X-Probe", "yes")
		_, _ = io.WriteString(w, "hello")
	}))
	defer srv.Close()

	resp, err := Send(context.Background(), srv.Client(), http.MethodGet, srv.URL, nil, http.Header{})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want 200", resp.StatusCode)
	}
	if resp.Headers.Get("X-Probe") != "yes" {
		t.Errorf("Headers[X-Probe] = %q, want %q", resp.Headers.Get("X-Probe"), "yes")
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	if string(body) != "hello" {
		t.Errorf("body = %q, want %q", body, "hello")
	}
	if resp.Duration <= 0 {
		t.Errorf("Duration = %v, want measured", resp.Duration)
	}
}

func TestSendRejectsUnsupportedScheme(t *testing.T) {
	if _, err := Send(context.Background(), DefaultClient(), http.MethodGet, "ftp://example.com/x", nil, nil); err == nil {
		t.Fatal("Send accepted a non-http(s) URL, want error")
	}
}
