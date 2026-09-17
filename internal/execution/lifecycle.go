package execution

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"req/internal/model"
	"req/internal/scripting"
)

const (
	// DefaultScriptTimeout bounds one script entry, including its
	// asynchronous work; --script-timeout overrides it.
	DefaultScriptTimeout = 5 * time.Second
	// MaxScriptBodyBytes caps how much of the decoded response body is
	// buffered when post-response scripts will run. ponytail: a constant
	// for now; configurability is a later phase.
	MaxScriptBodyBytes = 10 << 20
)

// ScriptPolicy configures script execution for one run.
type ScriptPolicy struct {
	Disabled bool          // --no-scripts: run nothing, stream unbounded
	Timeout  time.Duration // per-entry deadline; 0 = DefaultScriptTimeout
}

// InheritedScripts walks coll along segments (segments[0] is the collection
// name) and returns the enabled pre-request and post-response entries in
// execution order: collection, outer folder to inner folder, request. Only
// enabled entries are returned. The segments come from a successful
// ResolvePath, so lookup failure is impossible; a plain error is returned
// anyway rather than panicking.
func InheritedScripts(coll model.Collection, segments []string) (pre, post []model.Script, err error) {
	if len(segments) == 0 {
		return nil, nil, fmt.Errorf("empty path names no collection")
	}
	var levels []*model.Scripts
	levels = append(levels, coll.Scripts)
	children := coll.Items
	for i, seg := range segments[1:] {
		idx := -1
		for j := range children {
			if children[j].Name == seg {
				idx = j
				break
			}
		}
		if idx < 0 {
			return nil, nil, fmt.Errorf("no item named %q", seg)
		}
		it := children[idx]
		if i == len(segments)-2 { // final segment: its scripts close the chain
			if it.Type == "folder" {
				levels = append(levels, it.Folder.Scripts)
			} else {
				levels = append(levels, it.Request.Scripts)
			}
			break
		}
		if it.Type != "folder" {
			return nil, nil, fmt.Errorf("%q is a request and has no children", seg)
		}
		levels = append(levels, it.Folder.Scripts)
		children = it.Folder.Children
	}
	for _, s := range levels {
		if s == nil {
			continue
		}
		for _, script := range s.PreRequest {
			if script.Enabled {
				pre = append(pre, script)
			}
		}
		for _, script := range s.PostResponse {
			if script.Enabled {
				post = append(post, script)
			}
		}
	}
	return pre, post, nil
}

// phaseResult classifies how one script phase ended.
type phaseResult int

const (
	phaseDone     phaseResult = iota // every entry ran
	phaseCanceled                    // the outer context ended
	phaseFailed                      // a script raised a runtime error
	phaseSkipped                     // a script called pm.execution.skipRequest
)

// RunLifecycle runs pre scripts (unless disabled/absent), resolves and sends
// the request, then runs post scripts, choosing exit codes per the contract:
// cancellation 130 beats storage-free script failure 5, which beats failed
// assertions 6, which beats FailOnHTTPError 4 and transport failure 3. Pre
// scripts run against the execution copy before ResolveRequest, so a
// pre-script failure stops everything before variables resolve or anything
// is sent. With no scripts to run (or --no-scripts) the body streams exactly
// like Execute; otherwise it is buffered up to MaxScriptBodyBytes so post
// scripts can be run first.
func RunLifecycle(ctx context.Context, saved model.Request, ov Overrides, pol Policy, sp ScriptPolicy, pre, post []model.Script, stdout, stderr io.Writer) int {
	if saved.Import.Blocked() {
		fmt.Fprintf(stderr, "req: imported request is blocked: %s\n", strings.Join(saved.Import.Unsupported, "; "))
		return codeUsage
	}
	if sp.Disabled {
		pre, post = nil, nil
	}
	if len(pre) == 0 && len(post) == 0 {
		outgoing, err := Prepare(saved, ov, pol)
		if err != nil {
			fmt.Fprintf(stderr, "req: %v\n", err)
			return codeUsage
		}
		return Execute(ctx, outgoing, stdout, stderr)
	}

	// One engine per execution: VM global state is shared between the
	// scripts of this run but never leaks across invocations.
	eng := scripting.NewEngine(pol.Variables)
	defer eng.Close()

	// Pre scripts mutate this copy; the saved definition stays untouched.
	merged := MergeOverrides(saved, ov)
	eng.SetPreRequest(merged)

	timeout := sp.Timeout
	if timeout <= 0 {
		timeout = DefaultScriptTimeout
	}

	res, skipID, assertFailed := runPhase(ctx, eng, pre, timeout, stderr)
	switch res {
	case phaseCanceled:
		return codeCanceled
	case phaseFailed:
		return codeScript
	case phaseSkipped:
		fmt.Fprintf(stderr, "req: skipped by script %s\n", skipID)
		return codeSuccess
	}

	outgoing, err := ResolveRequest(merged, pol)
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return codeUsage
	}

	if len(post) == 0 {
		return preferAssertions(Execute(ctx, outgoing, stdout, stderr), assertFailed)
	}

	// The post phase needs the received response, so the body is buffered
	// instead of streamed.
	resp, code := dispatch(ctx, outgoing, stderr)
	if resp == nil {
		return preferAssertions(code, assertFailed)
	}
	defer resp.Body.Close()
	buf, err := io.ReadAll(io.LimitReader(resp.Body, MaxScriptBodyBytes+1))
	if err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(stderr, "req: canceled")
			return codeCanceled
		}
		fmt.Fprintf(stderr, "req: reading body: %v\n", err)
		return preferAssertions(codeTransport, assertFailed)
	}
	if len(buf) > MaxScriptBodyBytes {
		fmt.Fprintf(stderr, "req: response body exceeds the 10 MiB script buffer limit; post-response scripts were not run\n")
		return codeScript
	}
	if _, err := stdout.Write(buf); err != nil {
		fmt.Fprintf(stderr, "req: writing body: %v\n", err)
		return preferAssertions(codeTransport, assertFailed)
	}

	// The read-only post view: the resolved outgoing request, with the body
	// exposed only when it is inline raw/JSON (sharing the resolved text).
	view := &scripting.ExecRequest{
		Method:  outgoing.Method,
		URL:     outgoing.URL,
		Headers: outgoing.Headers,
	}
	if b := merged.Body; b != nil && b.File == "" && (b.Type == "raw" || b.Type == "json") {
		view.Body = b
	}
	eng.SetPostRequest(view, scripting.ResponseData{
		Code:    resp.StatusCode,
		Status:  http.StatusText(resp.StatusCode),
		TimeMS:  resp.Duration.Milliseconds(),
		Headers: resp.Headers,
		Body:    buf,
	})

	res, skipID, postFailed := runPhase(ctx, eng, post, timeout, stderr)
	assertFailed = assertFailed || postFailed
	switch res {
	case phaseCanceled:
		return codeCanceled
	case phaseFailed:
		return codeScript
	case phaseSkipped:
		fmt.Fprintf(stderr, "req: script %s failed: skipRequest is only allowed in pre-request scripts\n", skipID)
		return codeScript
	}
	if pol.FailOnHTTPError && resp.StatusCode >= http.StatusBadRequest {
		return preferAssertions(codeHTTPFail, assertFailed)
	}
	return preferAssertions(codeSuccess, assertFailed)
}

// preferAssertions applies 6 > 3 > 4 > 0 from the exit-code contract: a
// failed assertion outranks transport, --fail and success outcomes, but
// never cancellation or usage failures.
func preferAssertions(code int, assertFailed bool) int {
	if assertFailed && (code == codeSuccess || code == codeTransport || code == codeHTTPFail) {
		return codeAssertions
	}
	return code
}

// runPhase runs one script phase's entries in stored order on eng, each
// under its own timeout deadline, printing every entry's logs and pm.test
// outcomes prefixed with the entry ID. The first cancellation, runtime error
// or skip stops the phase; in the skip case the returned ID names the entry
// that asked. The third result reports whether any recorded test failed, so
// the caller can apply the exit-code contract even when the phase succeeded.
func runPhase(ctx context.Context, eng scripting.Engine, entries []model.Script, timeout time.Duration, stderr io.Writer) (res phaseResult, skipID string, assertFailed bool) {
	for _, entry := range entries {
		scriptCtx, cancel := context.WithTimeout(ctx, timeout)
		rep, err := eng.Run(scriptCtx, scripting.Source{Name: scriptSourceName(entry), Code: entry.Source})
		cancel()
		for _, line := range rep.Logs {
			fmt.Fprintf(stderr, "%s: %s\n", entry.ID, line)
		}
		for _, tr := range rep.Tests {
			if tr.Failed {
				assertFailed = true
				fmt.Fprintf(stderr, "%s: FAIL %s: %s\n", entry.ID, tr.Name, tr.Error)
			} else {
				fmt.Fprintf(stderr, "%s: PASS %s\n", entry.ID, tr.Name)
			}
		}
		if ctx.Err() != nil {
			fmt.Fprintln(stderr, "req: canceled")
			return phaseCanceled, "", assertFailed
		}
		if err != nil {
			fmt.Fprintf(stderr, "req: script %s failed: %v\n", entry.ID, err)
			return phaseFailed, "", assertFailed
		}
		if rep.Skipped {
			return phaseSkipped, entry.ID, assertFailed
		}
	}
	return phaseDone, "", assertFailed
}

func scriptSourceName(entry model.Script) string {
	if p := entry.Provenance; p != nil {
		if p.Source != "" && p.Path != "" {
			return p.Source + ":" + p.Path
		}
		if p.Path != "" {
			return p.Path
		}
		if p.Source != "" {
			return p.Source
		}
	}
	return entry.ID
}
