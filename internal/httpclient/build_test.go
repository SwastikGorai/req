package httpclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestClientPolicy(t *testing.T) {
	if c := Client(Options{}); c.Timeout != DefaultTimeout {
		t.Errorf("default Timeout = %v, want %v", c.Timeout, DefaultTimeout)
	}
	if c := Client(Options{Timeout: 3 * time.Second}); c.Timeout != 3*time.Second {
		t.Errorf("Timeout = %v, want 3s", c.Timeout)
	}
	if c := Client(Options{}); c.Transport != nil {
		t.Errorf("default Transport = %T, want nil (net/http default, TLS verification on)", c.Transport)
	}
	tr, ok := Client(Options{InsecureTLS: true}).Transport.(*http.Transport)
	if !ok || !tr.TLSClientConfig.InsecureSkipVerify {
		t.Errorf("InsecureTLS did not disable certificate verification")
	}
}

func TestSameOrigin(t *testing.T) {
	parse := func(s string) *url.URL {
		u, err := url.Parse(s)
		if err != nil {
			t.Fatal(err)
		}
		return u
	}
	cases := []struct {
		a, b string
		want bool
	}{
		{"http://api.example.com/x", "http://api.example.com/y", true},
		{"http://API.example.com/x", "http://api.example.com/y", true},
		{"http://127.0.0.1:8080/x", "http://127.0.0.1:9090/y", false}, // port change
		{"https://api.example.com/x", "http://api.example.com/y", false}, // downgrade
		{"http://example.com/x", "http://api.example.com/y", false},     // subdomain
	}
	for _, c := range cases {
		if got := sameOrigin(parse(c.a), parse(c.b)); got != c.want {
			t.Errorf("sameOrigin(%s, %s) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestClientRedirectPolicy(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusFound)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "done")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	resp, err := Send(context.Background(), Client(Options{FollowRedirects: true}), http.MethodGet, srv.URL+"/start", nil, nil)
	if err != nil {
		t.Fatalf("follow: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "done" {
		t.Errorf("follow: status %d body %q, want 200 done", resp.StatusCode, body)
	}

	resp, err = Send(context.Background(), Client(Options{FollowRedirects: false}), http.MethodGet, srv.URL+"/start", nil, nil)
	if err != nil {
		t.Fatalf("no-follow: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || resp.Headers.Get("Location") != "/final" {
		t.Errorf("no-follow: status %d Location %q, want 302 /final", resp.StatusCode, resp.Headers.Get("Location"))
	}
}
