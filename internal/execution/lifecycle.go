package execution

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/SwastikGorai/req/internal/model"
	"github.com/SwastikGorai/req/internal/output"
	"github.com/SwastikGorai/req/internal/scripting"
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
	Persist  func(context.Context) error
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

type phaseOutcome struct {
	result       phaseResult
	skipID       string
	assertFailed bool
	logs         []string
	tests        []scripting.TestResult
	errors       []string
}

// RunLifecycle runs pre scripts (unless disabled/absent), resolves and sends
// the request, then runs post scripts and optionally persists eligible variable
// changes, choosing exit codes per the contract: cancellation 130 beats
// persistence failure 7, which beats script failure 5, failed assertions 6,
// transport failure 3 and FailOnHTTPError 4. Pre scripts run against the
// execution copy before ResolveRequest, so a pre-script failure stops
// everything before variables resolve or anything is sent. With no scripts to
// run (or --no-scripts) the body streams exactly like Execute; otherwise it is
// buffered up to MaxScriptBodyBytes so post scripts can be run first.
func RunLifecycle(ctx context.Context, saved model.Request, ov Overrides, pol Policy, sp ScriptPolicy, pre, post []model.Script, stdout, stderr io.Writer) int {
	if saved.Import.Blocked() {
		message := fmt.Sprintf("imported request is blocked: %s", strings.Join(saved.Import.Unsupported, "; "))
		fmt.Fprintf(stderr, "req: %s\n", message)
		return finishLifecycle(ctx, pol.Output, output.Result{Errors: []string{message}}, codeUsage, stdout, stderr)
	}
	if sp.Disabled {
		pre, post = nil, nil
	}
	if len(pre) == 0 && len(post) == 0 {
		outgoing, err := Prepare(saved, ov, pol)
		if err != nil {
			fmt.Fprintf(stderr, "req: %v\n", err)
			return finishLifecycle(ctx, pol.Output, output.Result{Errors: []string{err.Error()}}, codeUsage, stdout, stderr)
		}
		state := executeResult(ctx, outgoing, stdout, stderr)
		eligible := state.code == codeSuccess || state.code == codeHTTPFail
		state.code = persistResult(ctx, sp, stderr, state.code, eligible, &state.result)
		return finishResult(ctx, outgoing, state, stdout, stderr)
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

	preOutcome := runPhase(ctx, eng, pre, timeout, stderr)
	if preOutcome.result != phaseDone {
		result := phaseOutput(preOutcome)
		code := phaseCode(preOutcome)
		if preOutcome.result == phaseSkipped {
			fmt.Fprintf(stderr, "req: skipped by script %s\n", preOutcome.skipID)
		}
		return finishLifecycle(ctx, pol.Output, result, code, stdout, stderr)
	}

	outgoing, err := ResolveRequest(merged, pol)
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		result := phaseOutput(preOutcome)
		result.Errors = append(result.Errors, err.Error())
		return finishLifecycle(ctx, pol.Output, result, codeUsage, stdout, stderr)
	}

	if len(post) == 0 {
		state := executeResult(ctx, outgoing, stdout, stderr)
		addPhaseOutput(&state.result, preOutcome)
		baseCode := state.code
		state.code = preferAssertions(baseCode, preOutcome.assertFailed)
		eligible := baseCode == codeSuccess || baseCode == codeHTTPFail
		state.code = persistResult(ctx, sp, stderr, state.code, eligible, &state.result)
		return finishResult(ctx, outgoing, state, stdout, stderr)
	}

	// The post phase needs the received response, so the body is buffered
	// instead of streamed.
	resp, code, dispatchErr := dispatch(ctx, outgoing, stderr)
	state := runState{code: code}
	if dispatchErr != nil {
		state.result.Errors = append(state.result.Errors, dispatchErr.Error())
	}
	if resp == nil {
		addPhaseOutput(&state.result, preOutcome)
		state.code = preferAssertions(state.code, preOutcome.assertFailed)
		return finishResult(ctx, outgoing, state, stdout, stderr)
	}
	defer resp.Body.Close()
	state.result = responseResult(resp)
	buf, err := readLimitedBody(resp.Body)
	if err != nil {
		addBodyError(&state, ctx, err)
		addPhaseOutput(&state.result, preOutcome)
		state.code = preferAssertions(state.code, preOutcome.assertFailed)
		fmt.Fprintf(stderr, "req: reading body: %v\n", err)
		if errors.Is(err, errBodyLimit) {
			fmt.Fprintln(stderr, "req: response body exceeds the 10 MiB script buffer limit; post-response scripts were not run")
		}
		return finishResult(ctx, outgoing, state, stdout, stderr)
	}
	state.result.Body, state.result.HasBody = buf, true
	if outgoing.Output.OutputPath != "" {
		if err := output.WriteFileAtomic(ctx, outgoing.Output.OutputPath, bytes.NewReader(buf)); err != nil {
			fmt.Fprintf(stderr, "req: writing output: %v\n", err)
			addBodyError(&state, ctx, err)
			state.result.Body = nil
			state.result.HasBody = false
		} else {
			state.result.Body = nil
			state.result.HasBody = false
			state.result.BodyPath = outgoing.Output.OutputPath
		}
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

	postOutcome := runPhase(ctx, eng, post, timeout, stderr)
	addPhaseOutput(&state.result, preOutcome)
	addPhaseOutput(&state.result, postOutcome)
	state.code = mergeExitCode(state.code, phaseCode(postOutcome))
	if postOutcome.result == phaseSkipped {
		state.result.Skipped = true
		state.result.Errors = append(state.result.Errors, "skipRequest is only allowed in pre-request scripts")
		state.code = mergeExitCode(state.code, codeScript)
		fmt.Fprintf(stderr, "req: script %s failed: skipRequest is only allowed in pre-request scripts\n", postOutcome.skipID)
	}
	if postOutcome.result == phaseFailed || postOutcome.result == phaseCanceled {
		return finishResult(ctx, outgoing, state, stdout, stderr)
	}
	if outgoing.FailOnHTTPError && resp.StatusCode >= http.StatusBadRequest {
		state.code = mergeExitCode(state.code, codeHTTPFail)
	}
	state.code = preferAssertions(state.code, preOutcome.assertFailed || postOutcome.assertFailed)
	eligible := postOutcome.result == phaseDone && (state.code == codeSuccess || state.code == codeHTTPFail || state.code == codeAssertions)
	state.code = persistResult(ctx, sp, stderr, state.code, eligible, &state.result)
	return finishResult(ctx, outgoing, state, stdout, stderr)
}

func phaseOutput(outcome phaseOutcome) output.Result {
	return output.Result{
		Logs:    append([]string(nil), outcome.logs...),
		Tests:   append([]scripting.TestResult(nil), outcome.tests...),
		Errors:  append([]string(nil), outcome.errors...),
		Skipped: outcome.result == phaseSkipped,
	}
}

func addPhaseOutput(result *output.Result, outcome phaseOutcome) {
	result.Logs = append(result.Logs, outcome.logs...)
	result.Tests = append(result.Tests, outcome.tests...)
	result.Errors = append(result.Errors, outcome.errors...)
	result.Skipped = result.Skipped || outcome.result == phaseSkipped
}

func phaseCode(outcome phaseOutcome) int {
	switch outcome.result {
	case phaseCanceled:
		return codeCanceled
	case phaseFailed:
		return codeScript
	case phaseSkipped:
		return codeSuccess
	default:
		return codeSuccess
	}
}

func finishLifecycle(ctx context.Context, opts output.Options, result output.Result, code int, stdout, stderr io.Writer) int {
	if err := output.Render(result, opts, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "req: writing output: %v\n", err)
		code = mergeExitCode(code, codeTransport)
	}
	return code
}

func persistResult(ctx context.Context, sp ScriptPolicy, stderr io.Writer, code int, eligible bool, result *output.Result) int {
	if !eligible || sp.Persist == nil {
		return code
	}
	if ctx.Err() != nil {
		fmt.Fprintln(stderr, "req: canceled")
		result.Errors = append(result.Errors, "canceled")
		return mergeExitCode(code, codeCanceled)
	}
	if err := sp.Persist(ctx); err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(stderr, "req: canceled")
			result.Errors = append(result.Errors, "canceled")
			return mergeExitCode(code, codeCanceled)
		}
		fmt.Fprintf(stderr, "req: persisting variables: %v\n", err)
		result.Errors = append(result.Errors, err.Error())
		return mergeExitCode(code, codeStorage)
	}
	if ctx.Err() != nil {
		fmt.Fprintln(stderr, "req: canceled")
		result.Errors = append(result.Errors, "canceled")
		return mergeExitCode(code, codeCanceled)
	}
	return code
}

// preferAssertions applies 6 > 3 > 4 > 0 from the exit-code contract: a
// failed assertion outranks transport, --fail and success outcomes, but
// never cancellation, usage or script failures.
func preferAssertions(code int, assertFailed bool) int {
	if assertFailed && (code == codeSuccess || code == codeTransport || code == codeHTTPFail) {
		return codeAssertions
	}
	return code
}

// runPhase runs one script phase's entries in stored order on eng, each under
// its own timeout deadline, printing every entry's logs and pm.test outcomes
// prefixed with the entry ID. The first cancellation, runtime error or skip
// stops the phase; in the skip case the returned ID names the entry that asked.
func runPhase(ctx context.Context, eng scripting.Engine, entries []model.Script, timeout time.Duration, stderr io.Writer) phaseOutcome {
	outcome := phaseOutcome{result: phaseDone}
	for _, entry := range entries {
		scriptCtx, cancel := context.WithTimeout(ctx, timeout)
		rep, err := eng.Run(scriptCtx, scripting.Source{Name: scriptSourceName(entry), Code: entry.Source})
		cancel()
		outcome.logs = append(outcome.logs, rep.Logs...)
		outcome.tests = append(outcome.tests, rep.Tests...)
		for _, line := range rep.Logs {
			fmt.Fprintf(stderr, "%s: %s\n", entry.ID, line)
		}
		for _, tr := range rep.Tests {
			if tr.Failed {
				outcome.assertFailed = true
				fmt.Fprintf(stderr, "%s: FAIL %s: %s\n", entry.ID, tr.Name, tr.Error)
			} else {
				fmt.Fprintf(stderr, "%s: PASS %s\n", entry.ID, tr.Name)
			}
		}
		if ctx.Err() != nil {
			fmt.Fprintln(stderr, "req: canceled")
			outcome.result = phaseCanceled
			outcome.errors = append(outcome.errors, "canceled")
			return outcome
		}
		if err != nil {
			message := fmt.Sprintf("script %s failed: %v", entry.ID, err)
			fmt.Fprintf(stderr, "req: %s\n", message)
			outcome.result = phaseFailed
			outcome.errors = append(outcome.errors, message)
			return outcome
		}
		if rep.Skipped {
			outcome.result = phaseSkipped
			outcome.skipID = entry.ID
			return outcome
		}
	}
	return outcome
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
