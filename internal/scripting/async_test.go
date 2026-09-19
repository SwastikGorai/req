package scripting

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SwastikGorai/req/internal/variables"
)

func TestCallbackAuthBeforeMain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_, _ = io.WriteString(w, `{"token":"t-1"}`)
		}
	}))
	defer srv.Close()

	scope := &variables.Scope{Local: map[string]any{}}
	e := NewEngine(scope)
	defer e.Close()
	e.SetPreRequest(&ExecRequest{Method: http.MethodGet, URL: srv.URL + "/main"})

	rep, err := e.Run(context.Background(), Source{Name: "token.js", Code: `
		pm.sendRequest("` + srv.URL + `/token", function (err, response) {
			if (err) throw new Error(String(err));
			pm.variables.set("token", response.json().token);
		});
	`})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if rep.Skipped {
		t.Fatal("callback run was unexpectedly skipped")
	}
	if got := scope.Local["token"]; got != "t-1" {
		t.Fatalf("token = %v, want t-1 before the main request is built", got)
	}

	if got := scope.ReplaceIn("Bearer {{token}}"); got != "Bearer t-1" {
		t.Fatalf("resolved main authorization = %q", got)
	}
}

func TestNestedCallbackWait(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/outer", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "outer")
	})
	mux.HandleFunc("/inner", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "inner")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	e := NewEngine(nil)
	defer e.Close()
	rep, err := e.Run(context.Background(), Source{Name: "nested.js", Code: `
		pm.sendRequest("` + srv.URL + `/outer", function (err, response) {
			if (err) throw err;
			console.log("outer");
			pm.sendRequest("` + srv.URL + `/inner", function (err, response) {
				if (err) throw err;
				console.log(response.text());
			});
			console.log("outer done");
		});
		console.log("script done");
	`})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	want := []string{"script done", "outer", "outer done", "inner"}
	if strings.Join(rep.Logs, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("Logs = %v, want %v", rep.Logs, want)
	}
}

func TestCallbackThrows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	e := NewEngine(nil)
	defer e.Close()
	_, err := e.Run(context.Background(), Source{Name: "throws.js", Code: `
		pm.sendRequest("` + srv.URL + `", function () { throw new Error("callback boom"); });
	`})
	if err == nil || !strings.Contains(err.Error(), "pm.sendRequest callback") || !strings.Contains(err.Error(), "callback boom") {
		t.Fatalf("Run error = %v, want callback failure", err)
	}
}

func TestRequestObjectConversion(t *testing.T) {
	var seen struct {
		method string
		header string
		auth   string
		body   string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		seen.method = r.Method
		seen.header = r.Header.Get("X-Token")
		seen.auth = r.Header.Get("Authorization")
		seen.body = string(body)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	scope := &variables.Scope{Local: map[string]any{"token": "abc"}}
	e := NewEngine(scope)
	defer e.Close()
	code := `
		pm.sendRequest({
			url: "` + srv.URL + `",
			method: "post",
			header: [{key: "X-Token", value: "{{token}}"}, {key: "X-Off", value: "no", disabled: true}],
			body: {mode: "urlencoded", urlencoded: [{key: "a", value: "1"}]},
			auth: {type: "bearer", bearer: [{key: "token", value: "{{token}}"}]}
		}, function (err, response) {
			if (err) throw err;
			pm.test("response", function () { pm.expect(response.json().ok).to.be.true; });
		});
	`
	rep, err := e.Run(context.Background(), Source{Name: "object.js", Code: code})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(rep.Tests) != 1 || rep.Tests[0].Failed {
		t.Fatalf("Tests = %+v, want one passing callback test", rep.Tests)
	}
	if seen.method != "POST" || seen.header != "abc" || seen.auth != "Bearer abc" || seen.body != "a=1" {
		t.Fatalf("request = %+v, want POST/X-Token/auth/urlencoded body", seen)
	}
}

func TestAuxiliaryConcurrencyAndLimit(t *testing.T) {
	var active, maximum atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		for {
			old := maximum.Load()
			if n <= old || maximum.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		active.Add(-1)
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	e := NewEngine(nil)
	defer e.Close()
	var script strings.Builder
	for i := 0; i < maxAuxiliaryRequests; i++ {
		script.WriteString(`pm.sendRequest("` + srv.URL + `", function (err) { if (err) throw err; });`)
	}
	rep, err := e.Run(context.Background(), Source{Name: "limit.js", Code: script.String()})
	if err != nil || rep.Skipped {
		t.Fatalf("Run = (%+v, %v), want success", rep, err)
	}
	if got := maximum.Load(); got > maxAuxiliaryConcurrent {
		t.Fatalf("maximum concurrent requests = %d, want <= %d", got, maxAuxiliaryConcurrent)
	}

	var tooMany strings.Builder
	for i := 0; i < maxAuxiliaryRequests+1; i++ {
		tooMany.WriteString(`pm.sendRequest("` + srv.URL + `", function () {});`)
	}
	_, err = e.Run(context.Background(), Source{Name: "too-many.js", Code: tooMany.String()})
	if err == nil || !strings.Contains(err.Error(), "maximum 20 auxiliary requests") {
		t.Fatalf("Run error = %v, want auxiliary request limit", err)
	}
}

func TestAuxiliaryResponseJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Answer", "yes")
		_, _ = io.WriteString(w, `{"value":42}`)
	}))
	defer srv.Close()
	e := NewEngine(nil)
	defer e.Close()
	rep, err := e.Run(context.Background(), Source{Name: "response.js", Code: `
		pm.sendRequest("` + srv.URL + `", function (err, response) {
			if (err) throw err;
			console.log(JSON.stringify({code: response.code, status: response.status, answer: response.headers.get("X-Answer"), value: response.json().value}));
		});
	`})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var got map[string]any
	if len(rep.Logs) != 1 || json.Unmarshal([]byte(rep.Logs[0]), &got) != nil {
		t.Fatalf("Logs = %v, want one response JSON log", rep.Logs)
	}
	if got["code"] != float64(200) || got["status"] != "OK" || got["answer"] != "yes" || got["value"] != float64(42) {
		t.Fatalf("response log = %v", got)
	}
}
