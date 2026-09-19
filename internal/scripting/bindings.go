package scripting

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/dop251/goja"

	"req/internal/model"
	"req/internal/variables"
)

// ExecRequest is the execution copy of a request shared with the execution
// package. MergeOverrides builds one mutable copy for the pre-request phase;
// the post-response view of the same type is read-only. Values may still
// contain {{references}}: resolution happens after the pre scripts ran.
type ExecRequest struct {
	Method  string
	URL     string
	Headers [][2]string
	Queries [][2]string
	Auth    *model.Auth
	Body    *model.Body // execution copy; scripts and resolution mutate it in place
}

// ResponseData is the received response as pm.response sees it.
type ResponseData struct {
	Code    int
	Status  string
	TimeMS  int64 // milliseconds
	Headers http.Header
	Body    []byte
}

// The script phases, set by SetPreRequest/SetPostRequest.
const (
	phasePre  = "pre"
	phasePost = "post"
)

// Console caps per run (IMPLEMENTATION.md section 8).
const (
	maxConsoleEntries = 1000
	maxConsoleBytes   = 1 << 20
)

// consoleTruncated is the single notice appended when a console cap is hit;
// further output for the run is dropped.
const consoleTruncated = "[req] console output truncated (limits: 1000 entries / 1 MiB); further output is dropped for this run"

// SetPreRequest attaches the mutable pre-request view and switches the
// engine to the pre-request phase. Called between runs, when no script can
// be on the stack.
func (e *engine) SetPreRequest(m *ExecRequest) {
	e.pre, e.post, e.resp, e.phase = m, nil, nil, phasePre
	e.refreshRequestObjects()
}

// SetPostRequest attaches the read-only request view and the received
// response, switching the engine to the post-response phase.
func (e *engine) SetPostRequest(view *ExecRequest, resp ResponseData) {
	e.post, e.pre, e.phase = view, nil, phasePost
	e.resp = &resp
	e.refreshRequestObjects()
}

// refreshRequestObjects rebuilds the pm.request and pm.response properties
// from the current phase state.
func (e *engine) refreshRequestObjects() {
	switch {
	case e.pre != nil:
		mustSet(e.pmTarget, "request", e.buildRequest(e.pre))
	case e.post != nil:
		mustSet(e.pmTarget, "request", e.buildRequest(e.post))
	default:
		mustSet(e.pmTarget, "request", goja.Null())
	}
	if e.resp != nil {
		mustSet(e.pmTarget, "response", e.buildResponse(e.resp))
	} else {
		mustSet(e.pmTarget, "response", goja.Null())
	}
}

// guardDef describes one allowlisted pm object. Unknown string properties
// raise the descriptive compatibility error, property writes are rejected as
// read-only, and unavailable may reject a known property for the current
// phase (e.g. pm.response before the response arrived). Symbol properties
// pass through to the target untouched.
type guardDef struct {
	target      *goja.Object
	path        string
	allowed     []string // sorted; keeps the error message deterministic
	unavailable func(prop string) error
	setError    error // replaces the default "<path> is read-only" message
}

// guarded wraps def.target in a proxy enforcing the pm compatibility
// contract.
func (e *engine) guarded(def guardDef) goja.Value {
	rt := e.rt
	readOnly := def.setError
	if readOnly == nil {
		readOnly = fmt.Errorf("%s is read-only", def.path)
	}
	return rt.ToValue(rt.NewProxy(def.target, &goja.ProxyTrapConfig{
		Get: func(target *goja.Object, prop string, _ goja.Value) goja.Value {
			if def.unavailable != nil {
				if err := def.unavailable(prop); err != nil {
					panic(rt.NewGoError(err))
				}
			}
			if !inSorted(def.allowed, prop) {
				panic(rt.NewGoError(e.unsupportedAPIError(def.path+"."+prop, def.allowed)))
			}
			return target.Get(prop)
		},
		Set: func(target *goja.Object, _ string, _ goja.Value, _ goja.Value) bool {
			panic(rt.NewGoError(readOnly))
		},
	}))
}

func inSorted(list []string, s string) bool {
	i := sort.SearchStrings(list, s)
	return i < len(list) && list[i] == s
}

// unsupportedAPIError builds the compatibility error for an API outside the
// supported surface: the API path, the script location when a call stack is
// available, the phase, and the sorted allowlist.
func (e *engine) unsupportedAPIError(api string, allowed []string) error {
	msg := fmt.Sprintf("unsupported pm API %q", api)
	if e.running && e.srcName != "" {
		at := e.srcName
		if frames := e.rt.CaptureCallStack(0, nil); len(frames) > 0 {
			if pos := frames[0].Position(); pos.Line > 0 {
				at = fmt.Sprintf("%s:%d", at, pos.Line)
			}
		}
		msg += " at " + at
	}
	phase := ""
	if e.phase != "" {
		phase = e.phase + " script; "
	}
	return fmt.Errorf("%s (%ssupported: %s)", msg, phase, strings.Join(allowed, ", "))
}

// installBindings registers the production surface: pm (variables,
// environment, collectionVariables, request, response, execution,
// test/expect/sendRequest) and console.
func (e *engine) installBindings() {
	rt := e.rt
	pmTarget := rt.NewObject()
	e.pmTarget = pmTarget

	exec := rt.NewObject()
	mustSet(exec, "skipRequest", func(goja.FunctionCall) goja.Value {
		e.skipRequested = true
		if e.runCancel != nil {
			e.runCancel()
		}
		panic(rt.NewGoError(errSkipRequest))
	})
	mustSet(pmTarget, "execution", e.guarded(guardDef{
		target: exec, path: "pm.execution", allowed: []string{"skipRequest"},
	}))
	mustSet(pmTarget, "variables", e.bindVariables())
	mustSet(pmTarget, "environment", e.bindVariableLayer("pm.environment",
		"no environment selected; pass --env to set environment variables",
		func() *map[string]any { return &e.scope.Environment }, e.scope.SetEnvironment, e.scope.UnsetEnvironment))
	mustSet(pmTarget, "collectionVariables", e.bindVariableLayer("pm.collectionVariables",
		"no collection variables in this context",
		func() *map[string]any { return &e.scope.Collection }, e.scope.SetCollection, e.scope.UnsetCollection))
	mustSet(pmTarget, "request", goja.Null())
	mustSet(pmTarget, "response", goja.Null())
	e.installAssertions()
	e.installSendRequest()

	pm := e.guarded(guardDef{
		target:  pmTarget,
		path:    "pm",
		allowed: []string{"collectionVariables", "environment", "execution", "expect", "request", "response", "sendRequest", "test", "variables"},
		unavailable: func(prop string) error {
			switch prop {
			case "request":
				if e.pre == nil && e.post == nil {
					return errors.New("pm.request is not available: no request is attached to this run")
				}
			case "response":
				if e.resp == nil {
					return errors.New("pm.response is not available in pre-request scripts; it is set after the response arrives")
				}
			}
			return nil
		},
	})
	if err := rt.Set("pm", pm); err != nil {
		panic(err)
	}

	logLine := func(call goja.FunctionCall) goja.Value {
		parts := make([]string, len(call.Arguments))
		for i, arg := range call.Arguments {
			parts[i] = arg.String()
		}
		e.addLog(strings.Join(parts, " "))
		return goja.Undefined()
	}
	console := rt.NewObject()
	for _, name := range []string{"log", "info", "warn", "error"} {
		mustSet(console, name, logLine)
	}
	if err := rt.Set("console", e.guarded(guardDef{
		target: console, path: "console", allowed: []string{"error", "info", "log", "warn"},
	})); err != nil {
		panic(err)
	}
}

// addLog appends one console line, enforcing the per-run entry and byte
// caps: once a cap is hit, one truncation notice is appended and the rest is
// dropped.
func (e *engine) addLog(line string) {
	if e.logsTruncated {
		return
	}
	if len(e.logs) >= maxConsoleEntries || e.logBytes+len(line) > maxConsoleBytes {
		e.logsTruncated = true
		e.logs = append(e.logs, consoleTruncated)
		return
	}
	e.logs = append(e.logs, line)
	e.logBytes += len(line)
}

// bindVariables builds pm.variables: get/has resolve the full precedence
// chain (CLI > local > environment > collection), set/unset affect only the
// execution-local layer, replaceIn substitutes resolvable references.
func (e *engine) bindVariables() goja.Value {
	rt := e.rt
	obj := rt.NewObject()
	mustSet(obj, "get", func(name string) goja.Value {
		v, ok := e.scope.Get(name)
		if !ok {
			return goja.Undefined()
		}
		if v == nil {
			return goja.Null() // ToValue(nil) is undefined; stored null must stay null
		}
		return rt.ToValue(v)
	})
	mustSet(obj, "has", func(name string) bool {
		_, ok := e.scope.Get(name)
		return ok
	})
	mustSet(obj, "set", func(call goja.FunctionCall) goja.Value {
		if e.scope.Local == nil {
			e.scope.Local = map[string]any{}
		}
		e.scope.Local[call.Argument(0).String()] = call.Argument(1).Export()
		return goja.Undefined()
	})
	mustSet(obj, "unset", func(name string) goja.Value {
		delete(e.scope.Local, name)
		return goja.Undefined()
	})
	mustSet(obj, "replaceIn", func(call goja.FunctionCall) goja.Value {
		v := replaceInValue(e.scope, call.Argument(0).Export())
		if v == nil {
			return goja.Null()
		}
		return rt.ToValue(v)
	})
	return e.guarded(guardDef{
		target: obj, path: "pm.variables",
		allowed: []string{"get", "has", "replaceIn", "set", "unset"},
	})
}

// replaceInValue walks strings inside maps and slices, substituting every
// resolvable reference. Keys are never rewritten; unknown placeholders stay
// intact; other value types pass through.
func replaceInValue(s *variables.Scope, v any) any {
	switch t := v.(type) {
	case string:
		return s.ReplaceIn(t)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = replaceInValue(s, val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = replaceInValue(s, val)
		}
		return out
	default:
		return t
	}
}

// bindVariableLayer builds pm.environment or pm.collectionVariables over one
// scope layer. Reads on a missing layer return undefined/false; writes fail
// with the unavailable message. Writes mutate the in-memory map only;
// persistence is handled by the execution lifecycle.
func (e *engine) bindVariableLayer(path, unavailable string, layer func() *map[string]any, set func(string, any), unset func(string)) goja.Value {
	rt := e.rt
	obj := rt.NewObject()
	mustSet(obj, "get", func(name string) goja.Value {
		if v, ok := (*layer())[name]; ok {
			if v == nil {
				return goja.Null()
			}
			return rt.ToValue(v)
		}
		return goja.Undefined()
	})
	mustSet(obj, "has", func(name string) bool {
		_, ok := (*layer())[name]
		return ok
	})
	mustSet(obj, "set", func(call goja.FunctionCall) goja.Value {
		if *layer() == nil {
			panic(rt.NewGoError(errors.New(unavailable)))
		}
		set(call.Argument(0).String(), call.Argument(1).Export())
		return goja.Undefined()
	})
	mustSet(obj, "unset", func(name string) goja.Value {
		if *layer() == nil {
			panic(rt.NewGoError(errors.New(unavailable)))
		}
		unset(name)
		return goja.Undefined()
	})
	return e.guarded(guardDef{
		target: obj, path: path, allowed: []string{"get", "has", "set", "unset"},
	})
}

// buildRequest builds the pm.request adapter over m: read-only method and
// url, a headers object that mutates m in the pre-request phase only, and a
// body object exposing inline raw/JSON text.
func (e *engine) buildRequest(m *ExecRequest) goja.Value {
	readOnly := errors.New("pm.request is read-only")
	if e.phase == phasePost {
		readOnly = errors.New("pm.request is read-only in post-response scripts")
	}
	obj := e.rt.NewObject()
	mustSet(obj, "method", m.Method)
	mustSet(obj, "url", e.buildRequestURL(m))
	mustSet(obj, "headers", e.buildRequestHeaders(m, readOnly))
	mustSet(obj, "body", e.buildRequestBody(m))
	return e.guarded(guardDef{
		target: obj, path: "pm.request",
		allowed:  []string{"body", "headers", "method", "url"},
		setError: readOnly,
	})
}

// buildRequestURL builds pm.request.url, whose toString returns the URL of
// the active view: pre scripts see the merged, still-unresolved URL, post
// scripts the final URL with queries appended.
func (e *engine) buildRequestURL(m *ExecRequest) goja.Value {
	rt := e.rt
	obj := rt.NewObject()
	mustSet(obj, "toString", func(goja.FunctionCall) goja.Value { return rt.ToValue(m.URL) })
	return e.guarded(guardDef{
		target: obj, path: "pm.request.url", allowed: []string{"toString"},
	})
}

// buildRequestHeaders builds pm.request.headers. get/has are case-
// insensitive first-match reads; add/upsert/remove mutate the execution copy
// and are rejected on the read-only post-response view.
func (e *engine) buildRequestHeaders(m *ExecRequest, readOnly error) goja.Value {
	rt := e.rt
	requireMutable := func() {
		if e.phase == phasePost {
			panic(rt.NewGoError(readOnly))
		}
	}
	obj := rt.NewObject()
	mustSet(obj, "get", func(name string) goja.Value {
		if i := headerIndex(m.Headers, name); i >= 0 {
			return rt.ToValue(m.Headers[i][1])
		}
		return goja.Undefined()
	})
	mustSet(obj, "has", func(name string) bool { return headerIndex(m.Headers, name) >= 0 })
	mustSet(obj, "add", func(name, value string) {
		requireMutable()
		m.Headers = append(m.Headers, [2]string{name, value})
	})
	mustSet(obj, "upsert", func(name, value string) {
		requireMutable()
		m.Headers = removeHeaderEntries(m.Headers, name)
		m.Headers = append(m.Headers, [2]string{name, value})
	})
	mustSet(obj, "remove", func(name string) {
		requireMutable()
		m.Headers = removeHeaderEntries(m.Headers, name)
	})
	return e.guarded(guardDef{
		target: obj, path: "pm.request.headers",
		allowed:  []string{"add", "get", "has", "remove", "upsert"},
		setError: readOnly,
	})
}

// headerIndex returns the index of the first header named name
// (case-insensitive), or -1.
func headerIndex(headers [][2]string, name string) int {
	for i, h := range headers {
		if strings.EqualFold(h[0], name) {
			return i
		}
	}
	return -1
}

// removeHeaderEntries drops every header named name (case-insensitive),
// preserving the order of the rest.
func removeHeaderEntries(headers [][2]string, name string) [][2]string {
	out := headers[:0]
	for _, h := range headers {
		if !strings.EqualFold(h[0], name) {
			out = append(out, h)
		}
	}
	return out
}

// buildRequestBody builds pm.request.body: null unless the execution copy is
// an inline raw/JSON body, otherwise an object with a live raw getter/setter
// over the body text. A raw write on a JSON body may produce invalid JSON;
// request resolution rejects that later.
func (e *engine) buildRequestBody(m *ExecRequest) goja.Value {
	b := m.Body
	if b == nil || (b.Type != "raw" && b.Type != "json") {
		return goja.Null()
	}
	rt := e.rt
	const allowed = "raw"
	unknown := func(prop string) error {
		return e.unsupportedAPIError("pm.request.body."+prop, []string{allowed})
	}
	body := rt.NewProxy(rt.NewObject(), &goja.ProxyTrapConfig{
		Get: func(_ *goja.Object, prop string, _ goja.Value) goja.Value {
			if prop != allowed {
				panic(rt.NewGoError(unknown(prop)))
			}
			if b.File != "" || b.Text == nil {
				return goja.Undefined() // file-backed bodies carry no inline text
			}
			return rt.ToValue(*b.Text)
		},
		Set: func(_ *goja.Object, prop string, value goja.Value, _ goja.Value) bool {
			if prop != allowed {
				panic(rt.NewGoError(unknown(prop)))
			}
			if e.phase == phasePost {
				panic(rt.NewGoError(errors.New("pm.request is read-only in post-response scripts")))
			}
			if b.File != "" {
				panic(rt.NewGoError(fmt.Errorf("pm.request.body.raw cannot replace a file-backed body (%s)", b.File)))
			}
			text := value.String()
			b.Text = &text
			return true
		},
	})
	return rt.ToValue(body)
}

// buildResponse builds the pm.response adapter over r.
func (e *engine) buildResponse(r *ResponseData) goja.Value {
	return e.buildResponseValue(r, false)
}

// buildResponseValue optionally makes the outer response object safe to pass
// through Promise resolution. Promise resolution probes an object’s "then"
// property; the normal compatibility proxy must reject unknown properties,
// so Promise results allow that one probe and return undefined.
func (e *engine) buildResponseValue(r *ResponseData, promiseSafe bool) goja.Value {
	rt := e.rt
	obj := rt.NewObject()
	mustSet(obj, "code", r.Code)
	mustSet(obj, "status", r.Status)
	mustSet(obj, "responseTime", r.TimeMS)

	headers := rt.NewObject()
	mustSet(headers, "get", func(name string) goja.Value {
		if vals, ok := r.Headers[http.CanonicalHeaderKey(name)]; ok && len(vals) > 0 {
			return rt.ToValue(vals[0])
		}
		return goja.Undefined()
	})
	mustSet(headers, "has", func(name string) bool {
		vals, ok := r.Headers[http.CanonicalHeaderKey(name)]
		return ok && len(vals) > 0
	})
	mustSet(obj, "headers", e.guarded(guardDef{
		target: headers, path: "pm.response.headers", allowed: []string{"get", "has"},
	}))

	mustSet(obj, "json", func(goja.FunctionCall) goja.Value {
		var v any
		if err := json.Unmarshal(r.Body, &v); err != nil {
			panic(rt.NewGoError(fmt.Errorf("pm.response.json(): the response body is not valid JSON: %v", err)))
		}
		return rt.ToValue(v)
	})
	mustSet(obj, "text", func(goja.FunctionCall) goja.Value { return rt.ToValue(string(r.Body)) })
	allowed := []string{"code", "headers", "json", "responseTime", "status", "text", "to"}
	if promiseSafe {
		mustSet(obj, "then", goja.Undefined())
		allowed = []string{"code", "headers", "json", "responseTime", "status", "text", "then", "to"}
	}

	// The status assertion chain: pm.response.to.have.status(code).
	have := rt.NewObject()
	mustSet(have, "status", func(expected int) {
		if r.Code != expected {
			panic(rt.NewGoError(fmt.Errorf("expected response to have status code %d but got %d", expected, r.Code)))
		}
	})
	to := rt.NewObject()
	mustSet(to, "have", e.guarded(guardDef{target: have, path: "pm.response.to.have", allowed: []string{"status"}}))
	mustSet(obj, "to", e.guarded(guardDef{target: to, path: "pm.response.to", allowed: []string{"have"}}))

	return e.guarded(guardDef{
		target: obj, path: "pm.response",
		allowed: allowed,
	})
}

// mustSet panics on the impossible failure of defining a constant key.
func mustSet(o *goja.Object, name string, value any) {
	if err := o.Set(name, value); err != nil {
		panic(err)
	}
}
