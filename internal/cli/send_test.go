package cli

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

func TestHTTPRepeatedQuery(t *testing.T) {
	gotQuery := make(chan url.Values, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery <- r.URL.Query()
	}))
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"send", "GET", srv.URL + "?keep=1", "--query", "tag=a", "--query", "tag=b",
	}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	q := <-gotQuery
	if got := q["tag"]; len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("repeated query = %v, want [a b]", got)
	}
	if q.Get("keep") != "1" {
		t.Errorf("existing query component lost: %v", q)
	}
}

func TestHTTPJSON(t *testing.T) {
	gotBody := make(chan []byte, 1)
	gotHeader := make(chan http.Header, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("server saw method %q, want POST", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		gotBody <- body
		gotHeader <- r.Header.Clone()
	}))
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"send", "POST", srv.URL, "--json", `{"name":"req"}`,
	}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if got := string(<-gotBody); got != `{"name":"req"}` {
		t.Errorf("payload = %q, want the exact JSON text", got)
	}
	h := <-gotHeader
	if h.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", h.Get("Content-Type"))
	}
}

func TestHTTPFailFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "nope")
	}))
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"send", "GET", srv.URL}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("without --fail: exit = %d, want 0", code)
	}
	if stdout.String() != "nope" {
		t.Errorf("stdout = %q, want the 400 body", stdout.String())
	}

	stdout, stderr = bytes.Buffer{}, bytes.Buffer{}
	code = Run(context.Background(), []string{"send", "GET", srv.URL, "--fail"}, &stdout, &stderr)
	if code != exitHTTPFail {
		t.Fatalf("with --fail: exit = %d, want 4", code)
	}
	if stdout.String() != "nope" {
		t.Errorf("with --fail: stdout = %q, want the 400 body", stdout.String())
	}
	if !bytes.Contains(stderr.Bytes(), []byte("400")) {
		t.Errorf("with --fail: stderr = %q, want the status line", stderr.String())
	}
}

func TestHTTPRedirectSensitiveHeaders(t *testing.T) {
	authSeen := make(chan string, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authSeen <- r.Header.Get("Authorization")
		_, _ = io.WriteString(w, "landing")
	}))
	defer target.Close()
	origin := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/landing", http.StatusFound)
	}))
	origin.Start()
	defer origin.Close()

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"send", "GET", origin.URL, "--header", "Authorization: Bearer sekrit",
	}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	select {
	case got := <-authSeen:
		if got != "" {
			t.Errorf("unrelated origin received Authorization %q, want none", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the redirect target was never reached")
	}
}

func TestHTTPRedirectSameOriginKeepsHeaders(t *testing.T) {
	authSeen := make(chan string, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/home", http.StatusFound)
	})
	mux.HandleFunc("/home", func(w http.ResponseWriter, r *http.Request) {
		authSeen <- r.Header.Get("Authorization")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{
		"send", "GET", srv.URL + "/start", "--header", "Authorization: Bearer sekrit",
	}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if got := <-authSeen; got != "Bearer sekrit" {
		t.Errorf("same-origin redirect lost Authorization: got %q", got)
	}
}

func TestSendValidation(t *testing.T) {
	base := "http://127.0.0.1:1/x" // never dialed; parsing fails first
	cases := map[string][]string{
		"missing URL":          {"send", "GET"},
		"missing method":       {"send", base},
		"missing METHOD flags": {"send"},
		"unknown flag":         {"send", "GET", base, "--bogus", "x"},
		"missing flag value":   {"send", "GET", base, "--query"},
		"bad header":           {"send", "GET", base, "--header", "NoColon"},
		"bad query":            {"send", "GET", base, "--query", "noseparator"},
		"conflicting bodies":   {"send", "POST", base, "--body", "x", "--json", "{}"},
		"invalid JSON":         {"send", "POST", base, "--json", "{nope"},
		"bad timeout":          {"send", "GET", base, "--timeout", "soon"},
		"zero timeout":         {"send", "GET", base, "--timeout", "0s"},
		"method twice":         {"send", "--method", "PUT", "GET", base},
		"URL twice":            {"send", "--method", "PUT", "--url", base, base},
		"bad method token":     {"send", "GE@T", base},
		"schemeless URL":       {"send", "GET", "example.com/x"},
		"unsupported scheme":   {"send", "GET", "ftp://example.com/x"},
		"too many positional":  {"send", "GET", base, "extra"},
	}
	for name, args := range cases {
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), args, &stdout, &stderr); code != exitUsage {
			t.Errorf("%s: exit = %d, want 2 (stderr: %s)", name, code, stderr.String())
		}
	}
}

func TestSendBodyModes(t *testing.T) {
	gotBody := make(chan []byte, 1)
	gotHeader := make(chan http.Header, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		gotBody <- body
		gotHeader <- r.Header.Clone()
	}))
	defer srv.Close()

	run := func(t *testing.T, args ...string) (http.Header, []byte) {
		t.Helper()
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), append([]string{"send", "POST", srv.URL}, args...), &stdout, &stderr)
		if code != exitSuccess {
			t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr.String())
		}
		return <-gotHeader, <-gotBody
	}

	h, body := run(t, "--body", "plain text")
	if string(body) != "plain text" {
		t.Errorf("raw body = %q, want %q", body, "plain text")
	}
	if h.Get("Content-Type") != "text/plain" {
		t.Errorf("raw Content-Type = %q, want text/plain", h.Get("Content-Type"))
	}

	h, body = run(t, "--body", "")
	if len(body) != 0 {
		t.Errorf("empty body = %q, want empty", body)
	}
	if h.Get("Content-Length") != "0" {
		t.Errorf("empty body Content-Length = %q, want 0 (empty differs from absent)", h.Get("Content-Length"))
	}

	h, _ = run(t, "--json", "{}", "--header", "Content-Type: application/vnd.api+json")
	if h.Get("Content-Type") != "application/vnd.api+json" {
		t.Errorf("explicit Content-Type = %q, want it to override the generated one", h.Get("Content-Type"))
	}
}

func TestSendRedirectNoFollow(t *testing.T) {
	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, "/final", http.StatusFound)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, "done")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"send", "GET", srv.URL + "/start", "--no-follow"}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("--no-follow: exit = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("--no-follow: server hits = %d, want 1", n)
	}
	if !bytes.Contains(stderr.Bytes(), []byte("302")) {
		t.Errorf("--no-follow: stderr = %q, want the 302 status line", stderr.String())
	}

	stdout, stderr = bytes.Buffer{}, bytes.Buffer{}
	code = Run(context.Background(), []string{"send", "GET", srv.URL + "/start"}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("default follow: exit = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if n := hits.Load(); n != 3 {
		t.Errorf("default follow: server hits = %d, want 3 (no-follow run + start and final of this run)", n)
	}
	if stdout.String() != "done" {
		t.Errorf("default follow: stdout = %q, want %q", stdout.String(), "done")
	}
}

func TestSendInsecureTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "tls-ok")
	}))
	defer srv.Close()

	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"send", "GET", srv.URL}, &stdout, &stderr)
	if code != exitTransport {
		t.Fatalf("verification on: exit = %d, want 3 (stderr: %s)", code, stderr.String())
	}

	stdout, stderr = bytes.Buffer{}, bytes.Buffer{}
	code = Run(context.Background(), []string{"send", "GET", srv.URL, "--insecure"}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("--insecure: exit = %d, want 0 (stderr: %s)", code, stderr.String())
	}
	if stdout.String() != "tls-ok" {
		t.Errorf("--insecure: stdout = %q, want %q", stdout.String(), "tls-ok")
	}
}

func TestSendTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(400 * time.Millisecond)
	}))
	defer srv.Close()

	start := time.Now()
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"send", "GET", srv.URL, "--timeout", "50ms"}, &stdout, &stderr)
	if code != exitTransport {
		t.Fatalf("exit = %d, want 3 (stderr: %s)", code, stderr.String())
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("timeout took %v, want the 50ms deadline", elapsed)
	}
}
