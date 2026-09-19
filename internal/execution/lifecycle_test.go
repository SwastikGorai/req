package execution

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SwastikGorai/req/internal/model"
	"github.com/SwastikGorai/req/internal/output"
	"github.com/SwastikGorai/req/internal/variables"
)

// scriptEntry builds one stored script entry.
func scriptEntry(id, code string, enabled bool) model.Script {
	return model.Script{ID: id, Source: code, Enabled: enabled}
}

// requestItem builds a request-type item carrying scripts.
func requestItem(name string, scripts *model.Scripts) model.Item {
	return model.Item{
		Type: "request", ID: model.NewID("req"), Name: name,
		Request: &model.Request{Method: "GET", URL: "https://example.com/ping", Scripts: scripts},
	}
}

// folderItem builds a folder-type item carrying scripts and children.
func folderItem(name string, scripts *model.Scripts, children ...model.Item) model.Item {
	return model.Item{
		Type: "folder", ID: model.NewID("fld"), Name: name,
		Folder: &model.Folder{Children: children, Scripts: scripts},
	}
}

// runLifecyclePol runs RunLifecycle with the given policies and returns the
// exit code plus both output streams.
func runLifecyclePol(t *testing.T, ctx context.Context, saved model.Request, pol Policy, sp ScriptPolicy, pre, post []model.Script) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := RunLifecycle(ctx, saved, Overrides{}, pol, sp, pre, post, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// runLifecycle runs RunLifecycle with default HTTP policy variables.
func runLifecycle(t *testing.T, ctx context.Context, saved model.Request, sp ScriptPolicy, pre, post []model.Script) (int, string, string) {
	t.Helper()
	return runLifecyclePol(t, ctx, saved, Policy{Variables: &variables.Scope{}, FollowRedirects: true}, sp, pre, post)
}

func TestInheritedScriptsOrder(t *testing.T) {
	coll := model.Collection{
		SchemaVersion: model.SchemaVersion,
		ID:            "coll",
		Name:          "API",
		Scripts: &model.Scripts{
			PreRequest: []model.Script{
				scriptEntry("c1", "one", true),
				scriptEntry("off", "skipped", false), // disabled: dropped
			},
			PostResponse: []model.Script{scriptEntry("c2", "two", true)},
		},
		Items: []model.Item{
			folderItem("Outer",
				&model.Scripts{PreRequest: []model.Script{scriptEntry("f1", "three", true)}},
				folderItem("Inner",
					&model.Scripts{PostResponse: []model.Script{scriptEntry("i1", "four", true)}},
					requestItem("Ping", &model.Scripts{
						PreRequest:   []model.Script{scriptEntry("r1", "five", true)},
						PostResponse: []model.Script{scriptEntry("r2", "six", true)},
					}),
				),
			),
		},
	}

	pre, post, err := InheritedScripts(coll, []string{"API", "Outer", "Inner", "Ping"})
	if err != nil {
		t.Fatalf("InheritedScripts: %v", err)
	}
	var ids []string
	for _, s := range pre {
		ids = append(ids, s.ID)
	}
	if want := []string{"c1", "f1", "r1"}; strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Errorf("pre order = %v, want %v", ids, want)
	}
	ids = ids[:0]
	for _, s := range post {
		ids = append(ids, s.ID)
	}
	if want := []string{"c2", "i1", "r2"}; strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Errorf("post order = %v, want %v", ids, want)
	}

	// A single segment collects only the collection's entries.
	pre, post, err = InheritedScripts(coll, []string{"API"})
	if err != nil {
		t.Fatalf("InheritedScripts collection root: %v", err)
	}
	if len(pre) != 1 || pre[0].ID != "c1" || len(post) != 1 || post[0].ID != "c2" {
		t.Errorf("collection root scripts = %v / %v, want c1 / c2", pre, post)
	}

	// A bad segment fails with a plain error even though ResolvePath makes
	// it impossible.
	if _, _, err := InheritedScripts(coll, []string{"API", "Nope"}); err == nil {
		t.Error("a missing segment must fail, not panic")
	}
}

func TestScriptHierarchy(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, "the-body")
	}))
	defer srv.Close()

	// Collection -> outer folder -> inner folder -> request, every level
	// carrying one pre and one post entry that log a distinct marker.
	coll := model.Collection{
		SchemaVersion: model.SchemaVersion,
		ID:            "coll",
		Name:          "API",
		Scripts:       phaseScripts("c"),
		Items: []model.Item{
			folderItem("Outer", phaseScripts("f"),
				folderItem("Inner", phaseScripts("i"),
					requestItem("Ping", phaseScripts("r")),
				),
			),
		},
	}
	pre, post, err := InheritedScripts(coll, []string{"API", "Outer", "Inner", "Ping"})
	if err != nil {
		t.Fatalf("InheritedScripts: %v", err)
	}

	code, stdout, stderr := runLifecycle(t, context.Background(), model.Request{Method: "GET", URL: srv.URL}, ScriptPolicy{}, pre, post)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stdout != "the-body" {
		t.Errorf("stdout = %q, want the body", stdout)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("server hits = %d, want 1", n)
	}

	status := strings.Index(stderr, "-> 200")
	if status < 0 {
		t.Fatalf("stderr has no status line: %q", stderr)
	}
	at := func(marker string) int { return strings.Index(stderr, marker) }
	prev := 0
	for _, marker := range []string{"c-pre: c-pre", "f-pre: f-pre", "i-pre: i-pre", "r-pre: r-pre"} {
		p := at(marker)
		if p < prev || p > status {
			t.Fatalf("pre log %q at %d breaks the required order before the status line at %d: %q", marker, p, status, stderr)
		}
		prev = p
	}
	prev = status
	for _, marker := range []string{"c-post: c-post", "f-post: f-post", "i-post: i-post", "r-post: r-post"} {
		p := at(marker)
		if p < prev {
			t.Fatalf("post log %q at %d breaks the required order after the status line at %d: %q", marker, p, status, stderr)
		}
		prev = p
	}
}

// phaseScripts builds one level's scripts logging <prefix>-pre and
// <prefix>-post.
func phaseScripts(prefix string) *model.Scripts {
	return &model.Scripts{
		PreRequest:   []model.Script{scriptEntry(prefix+"-pre", "console.log(\""+prefix+"-pre\")", true)},
		PostResponse: []model.Script{scriptEntry(prefix+"-post", "console.log(\""+prefix+"-post\")", true)},
	}
}

func TestPreErrorNoSend(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, "unreachable")
	}))
	defer srv.Close()

	pre := []model.Script{
		scriptEntry("first", `throw new Error("boom")`, true),
		scriptEntry("second", `console.log("later")`, true),
	}
	post := []model.Script{scriptEntry("post", `console.log("post-ran")`, true)}

	code, stdout, stderr := runLifecycle(t, context.Background(), model.Request{Method: "GET", URL: srv.URL}, ScriptPolicy{}, pre, post)
	if code != 5 {
		t.Fatalf("exit = %d, want 5", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty (nothing may be sent)", stdout)
	}
	if strings.Contains(stderr, "later") || strings.Contains(stderr, "post-ran") {
		t.Errorf("stderr = %q, want no later pre or post script output", stderr)
	}
	if !strings.Contains(stderr, "script first failed") {
		t.Errorf("stderr = %q, want the failing entry named", stderr)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("server hits = %d, want 0", n)
	}
}

func TestScriptSkip(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	}))
	defer srv.Close()

	pre := []model.Script{
		scriptEntry("skipper", `pm.execution.skipRequest()`, true),
		scriptEntry("after", `console.log("after-skip")`, true),
	}

	code, stdout, stderr := runLifecycle(t, context.Background(), model.Request{Method: "GET", URL: srv.URL}, ScriptPolicy{}, pre, nil)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "skipped by script skipper") {
		t.Errorf("stderr = %q, want the skip note", stderr)
	}
	if strings.Contains(stderr, "after-skip") {
		t.Errorf("stderr = %q, want the entry after the skip to be omitted", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("server hits = %d, want 0", n)
	}
}

func TestPostErrorStillReturnsBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "kept")
	}))
	defer srv.Close()

	post := []model.Script{scriptEntry("post", `throw new Error("after")`, true)}

	code, stdout, stderr := runLifecycle(t, context.Background(), model.Request{Method: "GET", URL: srv.URL}, ScriptPolicy{}, nil, post)
	if code != 5 {
		t.Fatalf("exit = %d, want 5", code)
	}
	if stdout != "kept" {
		t.Errorf("stdout = %q, want the received body", stdout)
	}
	if !strings.Contains(stderr, "-> 200") {
		t.Errorf("stderr = %q, want the status line", stderr)
	}
	if !strings.Contains(stderr, "script post failed") {
		t.Errorf("stderr = %q, want the failing entry named", stderr)
	}
}

func TestTransportFailureNoPostScripts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing listens on url anymore: transport failure

	post := []model.Script{scriptEntry("post", `console.log("post-ran")`, true)}

	code, stdout, stderr := runLifecycle(t, context.Background(), model.Request{Method: "GET", URL: url}, ScriptPolicy{}, nil, post)
	if code != 3 {
		t.Fatalf("exit = %d, want 3", code)
	}
	if strings.Contains(stderr, "post-ran") {
		t.Errorf("stderr = %q, want no post scripts after a transport failure", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestHTTPErrorStillRunsPostScripts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "missing")
	}))
	defer srv.Close()

	post := []model.Script{scriptEntry("post", `console.log("post-ran")`, true)}
	saved := model.Request{Method: "GET", URL: srv.URL}

	// A 404 is a response, not a transport failure: post scripts run and the
	// body is delivered.
	code, stdout, stderr := runLifecycle(t, context.Background(), saved, ScriptPolicy{}, nil, post)
	if code != 0 {
		t.Fatalf("without --fail: exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stdout != "missing" || !strings.Contains(stderr, "post-ran") {
		t.Errorf("stdout = %q, stderr = %q, want the body and the post log", stdout, stderr)
	}

	// With --fail the 404 exits 4 when no script failed.
	code, stdout, _ = runLifecyclePol(t, context.Background(), saved,
		Policy{Variables: &variables.Scope{}, FollowRedirects: true, FailOnHTTPError: true}, ScriptPolicy{}, nil, post)
	if code != 4 {
		t.Fatalf("with --fail: exit = %d, want 4", code)
	}
	if stdout != "missing" {
		t.Errorf("with --fail: stdout = %q, want the 404 body", stdout)
	}

	// A post-script error beats --fail.
	throwing := append([]model.Script(nil), post...)
	throwing[0] = scriptEntry("post", `throw new Error("after")`, true)
	code, stdout, stderr = runLifecyclePol(t, context.Background(), saved,
		Policy{Variables: &variables.Scope{}, FollowRedirects: true, FailOnHTTPError: true}, ScriptPolicy{}, nil, throwing)
	if code != 5 {
		t.Fatalf("with --fail and a post error: exit = %d, want 5 (stderr: %s)", code, stderr)
	}
	if stdout != "missing" {
		t.Errorf("with --fail and a post error: stdout = %q, want the 404 body", stdout)
	}
}

func TestScriptTimeout(t *testing.T) {
	// A broken watchdog must fail the test mechanically instead of hanging.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pre := []model.Script{scriptEntry("spinner", "for(;;){}", true)}

	start := time.Now()
	code, _, stderr := runLifecycle(t, ctx, model.Request{Method: "GET", URL: "https://example.com/ping"},
		ScriptPolicy{Timeout: 100 * time.Millisecond}, pre, nil)
	if code != 5 {
		t.Fatalf("exit = %d, want 5 (stderr: %s)", code, stderr)
	}
	if !strings.Contains(stderr, "script spinner failed") {
		t.Errorf("stderr = %q, want the deadline failure", stderr)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("the 100ms script deadline took %v to fire", elapsed)
	}
}

func TestSkipInPostPhase(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "body")
	}))
	defer srv.Close()

	post := []model.Script{scriptEntry("skipper", `pm.execution.skipRequest()`, true)}

	code, stdout, stderr := runLifecycle(t, context.Background(), model.Request{Method: "GET", URL: srv.URL}, ScriptPolicy{}, nil, post)
	if code != 5 {
		t.Fatalf("exit = %d, want 5", code)
	}
	if !strings.Contains(stderr, "skipRequest is only allowed in pre-request scripts") {
		t.Errorf("stderr = %q, want the pre-phase-only diagnostic", stderr)
	}
	if stdout != "body" {
		t.Errorf("stdout = %q, want the received body", stdout)
	}
}

func TestNoScriptsFlag(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, "streamed")
	}))
	defer srv.Close()

	pre := []model.Script{scriptEntry("pre", `console.log("pre-ran")`, true)}
	post := []model.Script{scriptEntry("post", `console.log("post-ran")`, true)}

	code, stdout, stderr := runLifecycle(t, context.Background(), model.Request{Method: "GET", URL: srv.URL},
		ScriptPolicy{Disabled: true}, pre, post)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if strings.Contains(stderr, "pre-ran") || strings.Contains(stderr, "post-ran") {
		t.Errorf("stderr = %q, want no script logs under --no-scripts", stderr)
	}
	if stdout != "streamed" {
		t.Errorf("stdout = %q, want the streamed body", stdout)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("server hits = %d, want 1", n)
	}
}

func TestBodyLimitNoPostScripts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), MaxScriptBodyBytes+1))
	}))
	defer srv.Close()

	pre := []model.Script{scriptEntry("pre", `console.log("pre-ran")`, true)}
	post := []model.Script{scriptEntry("post", `console.log("post-ran")`, true)}

	code, stdout, stderr := runLifecycle(t, context.Background(), model.Request{Method: "GET", URL: srv.URL}, ScriptPolicy{}, pre, post)
	if code != 5 {
		t.Fatalf("exit = %d, want 5 (stderr: %s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %d bytes, want none (the partial body must not print)", len(stdout))
	}
	if !strings.Contains(stderr, "10 MiB script buffer limit") {
		t.Errorf("stderr = %q, want the body-limit diagnostic", stderr)
	}
	if strings.Contains(stderr, "post-ran") {
		t.Errorf("stderr = %q, want no post scripts past the body limit", stderr)
	}
}

func TestLargeBodyScriptFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), MaxScriptBodyBytes+1))
	}))
	defer srv.Close()
	var stdout, stderr bytes.Buffer
	code := RunLifecycle(context.Background(), model.Request{Method: "GET", URL: srv.URL}, Overrides{}, Policy{
		Variables: &variables.Scope{}, FollowRedirects: true,
		Output: output.Options{Format: "json"},
	}, ScriptPolicy{}, nil, []model.Script{scriptEntry("post", `console.log("must-not-run")`, true)}, &stdout, &stderr)
	if code != codeScript {
		t.Fatalf("exit = %d, want %d (stderr=%q)", code, codeScript, stderr.String())
	}
	var envelope struct {
		Body   any      `json:"body"`
		Errors []string `json:"errors"`
		Logs   []string `json:"logs"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout = %q: %v", stdout.String(), err)
	}
	if envelope.Body != nil || len(envelope.Errors) == 0 || len(envelope.Logs) != 0 || strings.Contains(stdout.String(), "must-not-run") {
		t.Fatalf("body-limit envelope = %#v, stdout=%q", envelope, stdout.String())
	}
}

func TestCombinedErrorPrecedence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "missing")
	}))
	defer srv.Close()
	var stdout, stderr bytes.Buffer
	code := RunLifecycle(context.Background(), model.Request{Method: "GET", URL: srv.URL}, Overrides{}, Policy{
		Variables: &variables.Scope{}, FollowRedirects: true, FailOnHTTPError: true,
		Output: output.Options{Format: "json"},
	}, ScriptPolicy{
		Persist: func(context.Context) error { return errors.New("revision conflict") },
	}, nil, []model.Script{scriptEntry("post", `pm.test("status", function () { pm.expect(1).to.equal(2); });`, true)}, &stdout, &stderr)
	if code != codeStorage {
		t.Fatalf("exit = %d, want %d (stderr=%q)", code, codeStorage, stderr.String())
	}
	var envelope struct {
		Body   string   `json:"body"`
		Errors []string `json:"errors"`
		Tests  []struct {
			Failed bool `json:"failed"`
		} `json:"tests"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatalf("stdout = %q: %v", stdout.String(), err)
	}
	if envelope.Body != "missing" || len(envelope.Tests) != 1 || !envelope.Tests[0].Failed || len(envelope.Errors) != 1 || envelope.Errors[0] != "revision conflict" {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestDisabledEntriesDoNotRun(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	coll := model.Collection{
		SchemaVersion: model.SchemaVersion,
		ID:            "coll",
		Name:          "API",
		Scripts: &model.Scripts{PreRequest: []model.Script{
			scriptEntry("off", `console.log("off-mark")`, false),
			scriptEntry("on", `console.log("on-mark")`, true),
		}},
	}
	pre, _, err := InheritedScripts(coll, []string{"API"})
	if err != nil {
		t.Fatalf("InheritedScripts: %v", err)
	}
	if len(pre) != 1 || pre[0].ID != "on" {
		t.Fatalf("pre = %v, want only the enabled entry", pre)
	}

	code, _, stderr := runLifecycle(t, context.Background(), model.Request{Method: "GET", URL: srv.URL}, ScriptPolicy{}, pre, nil)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if strings.Contains(stderr, "off-mark") {
		t.Errorf("stderr = %q, want the disabled entry to be skipped", stderr)
	}
	if !strings.Contains(stderr, "on-mark") {
		t.Errorf("stderr = %q, want the enabled entry to run", stderr)
	}
	if n := hits.Load(); n != 1 {
		t.Errorf("server hits = %d, want 1", n)
	}
}

func TestPreHeaderMutation(t *testing.T) {
	var hits atomic.Int32
	type recorded struct {
		header http.Header
		body   []byte
	}
	got := make(chan recorded, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		body, _ := io.ReadAll(r.Body)
		got <- recorded{header: r.Header.Clone(), body: body}
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	body := `{"k":"{{v}}"}`
	saved := model.Request{
		Method: "POST", URL: srv.URL,
		Headers: []model.Entry{{Key: "X-Base", Value: "{{token}}", Enabled: true}},
		Body:    &model.Body{Type: "json", Text: &body},
	}
	before, err := json.Marshal(saved)
	if err != nil {
		t.Fatal(err)
	}

	pre := []model.Script{scriptEntry("pre",
		`pm.variables.set("token", "s3cret"); pm.variables.set("v", "1"); pm.request.headers.add("X-Generated", "by-script");`, true)}

	pol := Policy{Variables: &variables.Scope{}, FollowRedirects: true}
	code, stdout, stderr := runLifecyclePol(t, context.Background(), saved, pol, ScriptPolicy{}, pre, nil)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stdout != "ok" {
		t.Errorf("stdout = %q, want the server body", stdout)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("server hits = %d, want 1", n)
	}

	rec := <-got
	if rec.header.Get("X-Base") != "s3cret" {
		t.Errorf("server saw X-Base %q, want the pm.variables.set value resolved into the saved header", rec.header.Get("X-Base"))
	}
	if rec.header.Get("X-Generated") != "by-script" {
		t.Errorf("server saw X-Generated %q, want the pre-script generated header", rec.header.Get("X-Generated"))
	}
	if string(rec.body) != `{"k":"1"}` {
		t.Errorf("server saw body %q, want the pm.variables value resolved into the saved body", rec.body)
	}

	after, _ := json.Marshal(saved)
	if !bytes.Equal(before, after) {
		t.Errorf("RunLifecycle mutated the saved request definition:\nbefore %s\nafter  %s", before, after)
	}
}

func TestCallbackAuthBeforeMain(t *testing.T) {
	var mainHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = io.WriteString(w, `{"token":"t-1"}`)
			return
		}
		mainHeader = r.Header.Get("X-Token")
		_, _ = io.WriteString(w, "main")
	}))
	defer srv.Close()

	saved := model.Request{
		Method:  "GET",
		URL:     srv.URL + "/main",
		Headers: []model.Entry{{Key: "X-Token", Value: "{{token}}", Enabled: true}},
	}
	scope := &variables.Scope{Local: map[string]any{}}
	pre := []model.Script{scriptEntry("token", `
		pm.sendRequest("`+srv.URL+`/token", function (err, response) {
			if (err) throw err;
			pm.variables.set("token", response.json().token);
		});
	`, true)}
	code, stdout, stderr := runLifecyclePol(t, context.Background(), saved,
		Policy{Variables: scope, FollowRedirects: true}, ScriptPolicy{}, pre, nil)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stdout != "main" || mainHeader != "t-1" {
		t.Fatalf("main response/header = %q/%q, want main/t-1", stdout, mainHeader)
	}
}

func TestPromiseAuthBeforeMain(t *testing.T) {
	var mainHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			_, _ = io.WriteString(w, `{"token":"t-1"}`)
			return
		}
		mainHeader = r.Header.Get("X-Token")
		_, _ = io.WriteString(w, "main")
	}))
	defer srv.Close()

	saved := model.Request{
		Method:  "GET",
		URL:     srv.URL + "/main",
		Headers: []model.Entry{{Key: "X-Token", Value: "{{token}}", Enabled: true}},
	}
	scope := &variables.Scope{Local: map[string]any{}}
	pre := []model.Script{scriptEntry("token", `
		pm.sendRequest("`+srv.URL+`/token").then(function (response) {
			pm.variables.set("token", response.json().token);
		});
	`, true)}
	code, stdout, stderr := runLifecyclePol(t, context.Background(), saved,
		Policy{Variables: scope, FollowRedirects: true}, ScriptPolicy{}, pre, nil)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if stdout != "main" || mainHeader != "t-1" {
		t.Fatalf("main response/header = %q/%q, want main/t-1", stdout, mainHeader)
	}
}

func TestPostTokenExtraction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"token":"t-1"}`)
	}))
	defer srv.Close()

	scope := &variables.Scope{Local: map[string]any{}, Environment: map[string]any{}}
	post := []model.Script{scriptEntry("post", `pm.environment.set("token", pm.response.json().token);`, true)}

	pol := Policy{Variables: scope, FollowRedirects: true}
	code, stdout, stderr := runLifecyclePol(t, context.Background(), model.Request{Method: "GET", URL: srv.URL}, pol, ScriptPolicy{}, nil, post)
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	if got := scope.Environment["token"]; got != "t-1" {
		t.Errorf("scope.Environment[token] = %v, want t-1 written by the post script", got)
	}
	if stdout != `{"token":"t-1"}` {
		t.Errorf("stdout = %q, want the received body", stdout)
	}
}

func TestResponseUnavailableBeforeSend(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, "unreachable")
	}))
	defer srv.Close()

	pre := []model.Script{scriptEntry("pre", `pm.response.code;`, true)}

	code, stdout, stderr := runLifecycle(t, context.Background(), model.Request{Method: "GET", URL: srv.URL}, ScriptPolicy{}, pre, nil)
	if code != 5 {
		t.Fatalf("exit = %d, want 5 (stderr: %s)", code, stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty (nothing may be sent)", stdout)
	}
	if !strings.Contains(stderr, "pm.response is not available in pre-request scripts") {
		t.Errorf("stderr = %q, want the unavailability diagnostic", stderr)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("server hits = %d, want 0", n)
	}
}
