package scripting

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"req/internal/model"
	"req/internal/variables"
)

func strp(s string) *string { return &s }

// runScript runs one script and fails the test on any error.
func runScript(t *testing.T, e *engine, name, code string) Report {
	t.Helper()
	rep, err := e.Run(context.Background(), Source{Name: name, Code: code})
	if err != nil {
		t.Fatalf("Run %s: %v", name, err)
	}
	return rep
}

// mustFail runs one script and fails the test unless it errors with every
// fragment present in the error text.
func mustFail(t *testing.T, e *engine, name, code string, want ...string) {
	t.Helper()
	_, err := e.Run(context.Background(), Source{Name: name, Code: code})
	if err == nil {
		t.Fatalf("Run %s succeeded, want an error mentioning %v", name, want)
	}
	for _, s := range want {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error %q does not mention %q", err, s)
		}
	}
}

func TestVariablesBindings(t *testing.T) {
	scope := &variables.Scope{
		CLI:         map[string]any{"a": "cli", "cliOnly": "yes"},
		Local:       map[string]any{"b": "local", "n": nil},
		Environment: map[string]any{"c": "env", "b": "env-b"},
		Collection:  map[string]any{"d": "coll", "a": "coll-a"},
	}
	e := NewEngine(scope)
	defer e.Close()

	rep := runScript(t, e, "vars.js", `
		function desc(v) { return typeof v + ":" + String(v); }
		console.log(JSON.stringify([
			desc(pm.variables.get("a")),
			desc(pm.variables.get("b")),
			desc(pm.variables.get("c")),
			desc(pm.variables.get("d")),
			desc(pm.variables.get("cliOnly")),
			desc(pm.variables.get("missing")),
			desc(pm.variables.get("n")),
			pm.variables.has("n"),
			pm.variables.has("missing"),
		]));
	`)
	want := `["string:cli","string:local","string:env","string:coll","string:yes","undefined:undefined","object:null",true,false]`
	if len(rep.Logs) != 1 || rep.Logs[0] != want {
		t.Errorf("Logs = %v, want [%s] (missing is undefined, stored null stays null)", rep.Logs, want)
	}

	// set writes the local layer only; the CLI layer still wins resolution,
	// and unsetting a local key lets the environment layer show through.
	rep = runScript(t, e, "mutate.js", `
		pm.variables.set("a", "from-local");
		pm.variables.set("new", "added");
		pm.variables.unset("b");
		console.log(JSON.stringify([pm.variables.get("a"), pm.variables.get("b"), pm.variables.get("new")]));
	`)
	want = `["cli","env-b","added"]`
	if len(rep.Logs) != 1 || rep.Logs[0] != want {
		t.Errorf("Logs = %v, want [%s]", rep.Logs, want)
	}
	if scope.Local["a"] != "from-local" {
		t.Errorf("scope.Local[a] = %v, want the pm.variables.set value", scope.Local["a"])
	}
	if scope.CLI["a"] != "cli" {
		t.Errorf("scope.CLI[a] = %v, want the CLI layer untouched", scope.CLI["a"])
	}
	if _, ok := scope.Local["b"]; ok {
		t.Error("unset must delete from the local layer only")
	}
	if scope.Environment["b"] != "env-b" {
		t.Errorf("scope.Environment[b] = %v, want the environment layer untouched", scope.Environment["b"])
	}
}

func TestEnvironmentAndCollectionLayers(t *testing.T) {
	t.Run("missing layer", func(t *testing.T) {
		e := NewEngine(&variables.Scope{Local: map[string]any{}})
		defer e.Close()

		rep := runScript(t, e, "layers.js", `
			console.log(JSON.stringify([
				String(pm.environment.get("x")),
				pm.environment.has("x"),
				String(pm.collectionVariables.get("x")),
				pm.collectionVariables.has("x"),
			]));
		`)
		if want := `["undefined",false,"undefined",false]`; len(rep.Logs) != 1 || rep.Logs[0] != want {
			t.Errorf("Logs = %v, want [%s]", rep.Logs, want)
		}
		mustFail(t, e, "envset.js", `pm.environment.set("x", "1")`,
			"no environment selected; pass --env to set environment variables")
		mustFail(t, e, "envunset.js", `pm.environment.unset("x")`,
			"no environment selected; pass --env to set environment variables")
		mustFail(t, e, "collset.js", `pm.collectionVariables.set("x", "1")`,
			"no collection variables in this context")
		mustFail(t, e, "collunset.js", `pm.collectionVariables.unset("x")`,
			"no collection variables in this context")
	})

	t.Run("selected layer", func(t *testing.T) {
		env := map[string]any{"c": "env"}
		coll := map[string]any{"d": "coll"}
		e := NewEngine(&variables.Scope{Environment: env, Collection: coll})
		defer e.Close()

		runScript(t, e, "layers.js", `
			pm.environment.set("token", "t-1");
			pm.collectionVariables.set("base", "https://x");
			pm.environment.unset("c");
			console.log(JSON.stringify([pm.environment.get("token"), pm.collectionVariables.get("base"), pm.environment.has("c")]));
		`)
		if env["token"] != "t-1" {
			t.Errorf("env[token] = %v, want the in-memory write", env["token"])
		}
		if coll["base"] != "https://x" {
			t.Errorf("coll[base] = %v, want the in-memory write", coll["base"])
		}
		if _, ok := env["c"]; ok {
			t.Error("unset must delete from the environment layer")
		}
	})
}

func TestReplaceInBinding(t *testing.T) {
	e := NewEngine(&variables.Scope{Local: map[string]any{"who": "world", "n": int64(2)}})
	defer e.Close()

	rep := runScript(t, e, "replace.js", `
		console.log(pm.variables.replaceIn("hello {{who}} {{missing}}"));
		console.log(JSON.stringify(pm.variables.replaceIn({
			greeting: "hi {{who}}",
			kept: 7,
			nested: ["{{n}}", {deep: "{{missing}}"}],
		})));
		console.log(JSON.stringify(pm.variables.replaceIn({"{{who}}": "x", v: "{{who}}"})));
	`)
	// Objects are compared parsed: JSON key order is not part of the contract.
	want := []string{
		"hello world {{missing}}",
		`{"greeting":"hi world","kept":7,"nested":["2",{"deep":"{{missing}}"}]}`,
		`{"v":"world","{{who}}":"x"}`,
	}
	if len(rep.Logs) != len(want) {
		t.Fatalf("Logs = %v, want %v", rep.Logs, want)
	}
	for i := range want {
		if !strings.HasPrefix(want[i], "{") { // the plain string line matches verbatim
			if rep.Logs[i] != want[i] {
				t.Errorf("Logs[%d] = %q, want %q", i, rep.Logs[i], want[i])
			}
			continue
		}
		var got, expected any
		if err := json.Unmarshal([]byte(rep.Logs[i]), &got); err != nil {
			t.Fatalf("Logs[%d] = %q is not the expected JSON: %v", i, rep.Logs[i], err)
		}
		if err := json.Unmarshal([]byte(want[i]), &expected); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, expected) {
			t.Errorf("Logs[%d] = %q, want %q (keys stay, unknown placeholders stay)", i, rep.Logs[i], want[i])
		}
	}
}

func TestConsoleCaps(t *testing.T) {
	t.Run("entry cap", func(t *testing.T) {
		e := NewEngine(nil)
		defer e.Close()

		rep := runScript(t, e, "caps.js", `
			for (var i = 0; i < 999; i++) console.log("line " + i);
			console.log("the 1000th");
			console.log("one past the cap");
			console.log("and more");
		`)
		if len(rep.Logs) != maxConsoleEntries+1 {
			t.Fatalf("Logs = %d entries, want %d (1000 lines + one notice)", len(rep.Logs), maxConsoleEntries+1)
		}
		if rep.Logs[999] != "the 1000th" {
			t.Errorf("Logs[999] = %q, want the last line before the cap", rep.Logs[999])
		}
		if !strings.Contains(rep.Logs[1000], "truncated") {
			t.Errorf("Logs[1000] = %q, want exactly one truncation notice", rep.Logs[1000])
		}
	})

	t.Run("byte cap", func(t *testing.T) {
		e := NewEngine(nil)
		defer e.Close()

		rep := runScript(t, e, "bytes.js", `console.log("x".repeat(1024 * 1024 + 1)); console.log("dropped");`)
		if len(rep.Logs) != 1 || !strings.Contains(rep.Logs[0], "truncated") {
			t.Errorf("Logs = %v, want only the truncation notice", rep.Logs)
		}
	})
}

func TestUnknownPMAPI(t *testing.T) {
	t.Run("location and phase in the error", func(t *testing.T) {
		e := NewEngine(nil)
		defer e.Close()
		e.SetPreRequest(&ExecRequest{Method: "GET", URL: "http://x/"})

		mustFail(t, e, "unsupported.js", `pm.sendRequest("ftp://x/", function () {})`,
			"URL must be absolute http or https")
	})

	for _, tc := range []struct{ name, code, want string }{
		{"pm.foo", `pm.foo`, `unsupported pm API "pm.foo" at api.js:1 (pre script; supported: collectionVariables, environment, execution, expect, request, response, sendRequest, test, variables)`},
		{"pm.variables.typo", `pm.variables.typo`, `unsupported pm API "pm.variables.typo" at api.js:1 (pre script; supported: get, has, replaceIn, set, unset)`},
		{"pm.execution.typo", `pm.execution.typo`, `unsupported pm API "pm.execution.typo" at api.js:1 (pre script; supported: skipRequest)`},
		{"console.typo", `console.typo`, `unsupported pm API "console.typo" at api.js:1 (pre script; supported: error, info, log, warn)`},
		{"pm.request.typo", `pm.request.typo`, `unsupported pm API "pm.request.typo" at api.js:1 (pre script; supported: body, headers, method, url)`},
		{"write to pm", `pm.variables = null`, "pm is read-only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := NewEngine(nil)
			defer e.Close()
			e.SetPreRequest(&ExecRequest{Method: "GET", URL: "http://x/"})
			mustFail(t, e, "api.js", tc.code, tc.want)
		})
	}
}

func TestRequestHeaders(t *testing.T) {
	m := &ExecRequest{
		Method: "GET", URL: "http://x/ping",
		Headers: [][2]string{{"X-A", "1"}, {"x-b", "2"}, {"X-A", "3"}},
	}
	e := NewEngine(nil)
	defer e.Close()
	e.SetPreRequest(m)

	rep := runScript(t, e, "headers.js", `
		console.log(JSON.stringify([
			pm.request.method,
			pm.request.url.toString(),
			pm.request.headers.get("x-a"),
			pm.request.headers.has("X-B"),
			pm.request.headers.has("nope"),
		]));
		pm.request.headers.add("X-C", "4");
		pm.request.headers.upsert("X-A", "9");
		pm.request.headers.remove("x-b");
	`)
	if want := `["GET","http://x/ping","1",true,false]`; len(rep.Logs) != 1 || rep.Logs[0] != want {
		t.Errorf("Logs = %v, want [%s]", rep.Logs, want)
	}
	if got := [][2]string{{"X-C", "4"}, {"X-A", "9"}}; !reflect.DeepEqual(m.Headers, got) {
		t.Errorf("m.Headers = %v, want %v (upsert replaces all case-insensitive matches)", m.Headers, got)
	}
}

func TestRequestBody(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    *model.Body
		code    string
		wantLog string
		wantErr string
		want    *model.Body
	}{
		{"raw read", &model.Body{Type: "raw", Text: strp("hello")}, `console.log(String(pm.request.body.raw));`, "hello", "", nil},
		{"json read", &model.Body{Type: "json", Text: strp(`{"a":1}`)}, `console.log(String(pm.request.body.raw));`, `{"a":1}`, "", nil},
		{"raw write", &model.Body{Type: "raw"}, `pm.request.body.raw = "written";`, "", "", &model.Body{Type: "raw", Text: strp("written")}},
		{"json write may be invalid", &model.Body{Type: "json", Text: strp(`{"a":1}`)}, `pm.request.body.raw = "{oops";`, "", "", &model.Body{Type: "json", Text: strp("{oops")}},
		{"file-backed read is undefined", &model.Body{Type: "raw", File: "data.bin"}, `console.log(String(pm.request.body.raw));`, "undefined", "", nil},
		{"file-backed write fails", &model.Body{Type: "raw", File: "data.bin"}, `pm.request.body.raw = "x";`, "", "file-backed", nil},
		{"urlencoded body is null", &model.Body{Type: "urlencoded", URLEncoded: []model.Entry{{Key: "a", Value: "b", Enabled: true}}}, `console.log(String(pm.request.body));`, "null", "", nil},
		{"no body is null", nil, `console.log(String(pm.request.body));`, "null", "", nil},
		{"write to null body fails", nil, `pm.request.body.raw = "x";`, "", "TypeError", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := &ExecRequest{Method: "POST", URL: "http://x/", Body: tc.body}
			e := NewEngine(nil)
			defer e.Close()
			e.SetPreRequest(m)

			if tc.wantErr != "" {
				mustFail(t, e, "body.js", tc.code, tc.wantErr)
				return
			}
			rep := runScript(t, e, "body.js", tc.code)
			if strings.Contains(tc.code, "console.log") {
				if len(rep.Logs) != 1 || rep.Logs[0] != tc.wantLog {
					t.Errorf("Logs = %v, want [%s]", rep.Logs, tc.wantLog)
				}
			}
			if tc.want != nil && !reflect.DeepEqual(m.Body, tc.want) {
				t.Errorf("m.Body = %+v, want %+v", m.Body, tc.want)
			}
		})
	}
}

func TestPostPhaseReadOnlyRequest(t *testing.T) {
	for _, tc := range []struct{ name, code string }{
		{"headers add", `pm.request.headers.add("X-N", "1")`},
		{"headers upsert", `pm.request.headers.upsert("X-A", "2")`},
		{"headers remove", `pm.request.headers.remove("X-A")`},
		{"method write", `pm.request.method = "POST"`},
		{"body raw write", `pm.request.body.raw = "x"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := &ExecRequest{
				Method: "GET", URL: "http://x/final?a=1",
				Headers: [][2]string{{"X-A", "1"}},
				Body:    &model.Body{Type: "raw", Text: strp("t")},
			}
			e := NewEngine(nil)
			defer e.Close()
			e.SetPostRequest(view, ResponseData{Code: 200, Status: "OK"})
			mustFail(t, e, "post.js", tc.code, "pm.request is read-only in post-response scripts")
		})
	}
}

func TestResponseBindings(t *testing.T) {
	e := NewEngine(nil)
	defer e.Close()
	e.SetPostRequest(&ExecRequest{Method: "GET", URL: "http://x/final"},
		ResponseData{
			Code: 201, Status: "Created", TimeMS: 42,
			Headers: http.Header{"Content-Type": {"application/json"}},
			Body:    []byte(`{"a":1,"s":"x"}`),
		})

	rep := runScript(t, e, "resp.js", `
		console.log([pm.response.code, pm.response.status, pm.response.responseTime].join("|"));
		console.log(String(pm.response.headers.get("content-type")));
		console.log(String(pm.response.headers.has("content-type")));
		console.log(String(pm.response.headers.has("nope")));
		console.log(JSON.stringify(pm.response.json()));
		console.log(pm.response.text());
	`)
	want := []string{
		"201|Created|42",
		"application/json",
		"true",
		"false",
		`{"a":1,"s":"x"}`,
		`{"a":1,"s":"x"}`,
	}
	if len(rep.Logs) != len(want) {
		t.Fatalf("Logs = %v, want %v", rep.Logs, want)
	}
	for i := range want {
		// json() output is compared parsed: JSON key order is not part of the
		// contract; text() output must match byte for byte.
		if i == len(want)-2 {
			var got, expected any
			if err := json.Unmarshal([]byte(rep.Logs[i]), &got); err != nil {
				t.Fatalf("Logs[%d] = %q is not the expected JSON: %v", i, rep.Logs[i], err)
			}
			if err := json.Unmarshal([]byte(want[i]), &expected); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, expected) {
				t.Errorf("Logs[%d] = %q, want %q", i, rep.Logs[i], want[i])
			}
			continue
		}
		if rep.Logs[i] != want[i] {
			t.Errorf("Logs[%d] = %q, want %q", i, rep.Logs[i], want[i])
		}
	}

	// A body that is not JSON fails json() with a descriptive error.
	bad := NewEngine(nil)
	defer bad.Close()
	bad.SetPostRequest(&ExecRequest{}, ResponseData{Code: 200, Status: "OK", Body: []byte("not json")})
	mustFail(t, bad, "badjson.js", `pm.response.json()`, "pm.response.json(): the response body is not valid JSON")

	// pm.response is unavailable before the response arrived.
	pre := NewEngine(nil)
	defer pre.Close()
	pre.SetPreRequest(&ExecRequest{Method: "GET", URL: "http://x/"})
	mustFail(t, pre, "pre.js", `pm.response.code`,
		"pm.response is not available in pre-request scripts; it is set after the response arrives")
}
