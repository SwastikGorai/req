package execution

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/SwastikGorai/req/internal/httpclient"
	"github.com/SwastikGorai/req/internal/output"
)

// Exit codes returned by Execute, matching the CLI's contract.
const (
	codeSuccess    = 0
	codeUsage      = 2
	codeTransport  = 3
	codeHTTPFail   = 4
	codeScript     = 5 // a pre/post script failed, or the body hit the script buffer limit
	codeAssertions = 6 // failed pm.test assertions
	codeStorage    = 7 // variable persistence failed or conflicted
	codeCanceled   = 130
)

var errBodyLimit = errors.New("response body exceeds the 10 MiB script buffer limit")

type runState struct {
	result   output.Result
	code     int
	rendered bool
}

// Execute sends o and renders the result exactly like `req send`: the status
// line goes to stderr, the response body to stdout. A body-read error or a
// transport error exits 3, a canceled context exits 130, and FailOnHTTPError
// turns a status >= 400 into exit 4.
func Execute(ctx context.Context, o Outgoing, stdout, stderr io.Writer) int {
	state := executeResult(ctx, o, stdout, stderr)
	return finishResult(ctx, o, state, stdout, stderr)
}

// executeResult performs the exchange and consumes the body according to the
// output mode. Rendering is deferred so lifecycle callers can add script and
// persistence results to one envelope.
func executeResult(ctx context.Context, o Outgoing, stdout, stderr io.Writer) runState {
	resp, code, dispatchErr := dispatch(ctx, o, stderr)
	state := runState{code: code}
	if dispatchErr != nil {
		state.result.Errors = append(state.result.Errors, dispatchErr.Error())
	}
	if resp == nil {
		return state
	}
	defer resp.Body.Close()
	state.result = responseResult(resp)

	if o.Output.OutputPath != "" {
		if err := output.WriteFileAtomic(ctx, o.Output.OutputPath, resp.Body); err != nil {
			fmt.Fprintf(stderr, "req: writing output: %v\n", err)
			addBodyError(&state, ctx, err)
			return state
		}
		state.result.BodyPath = o.Output.OutputPath
	} else if o.Output.JSON() {
		body, err := readLimitedBody(resp.Body)
		if err != nil {
			fmt.Fprintf(stderr, "req: reading body: %v\n", err)
			addBodyError(&state, ctx, err)
			return state
		}
		state.result.Body, state.result.HasBody = body, true
	} else if o.Output.Terminal && !o.Output.Raw {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			fmt.Fprintf(stderr, "req: reading body: %v\n", err)
			addBodyError(&state, ctx, err)
			return state
		}
		state.result.Body, state.result.HasBody = body, true
	} else {
		if err := output.Render(state.result, o.Output, io.Discard, stderr); err != nil {
			addRenderError(&state, ctx, err)
		}
		state.rendered = true
		if _, err := io.Copy(stdout, resp.Body); err != nil {
			fmt.Fprintf(stderr, "req: reading body: %v\n", err)
			addBodyError(&state, ctx, err)
			return state
		}
	}
	if o.FailOnHTTPError && resp.StatusCode >= http.StatusBadRequest {
		state.code = mergeExitCode(state.code, codeHTTPFail)
	}
	return state
}

func responseResult(resp *httpclient.Response) output.Result {
	entries := resp.HeaderEntries
	if len(entries) == 0 {
		for name, values := range resp.Headers {
			for _, value := range values {
				entries = append(entries, [2]string{name, value})
			}
		}
	}
	return output.Result{
		StatusCode:  resp.StatusCode,
		StatusText:  http.StatusText(resp.StatusCode),
		Headers:     entries,
		Duration:    resp.Duration,
		ContentType: resp.Headers.Get("Content-Type"),
	}
}

func readLimitedBody(body io.Reader) ([]byte, error) {
	buf, err := io.ReadAll(io.LimitReader(body, MaxScriptBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(buf) > MaxScriptBodyBytes {
		return nil, errBodyLimit
	}
	return buf, nil
}

func addBodyError(state *runState, ctx context.Context, err error) {
	if ctx.Err() != nil {
		state.code = mergeExitCode(state.code, codeCanceled)
		state.result.Errors = append(state.result.Errors, "canceled")
		return
	}
	state.result.Errors = append(state.result.Errors, err.Error())
	var fileErr *output.FileError
	switch {
	case errors.Is(err, errBodyLimit):
		state.code = mergeExitCode(state.code, codeScript)
	case errors.As(err, &fileErr):
		state.code = mergeExitCode(state.code, codeStorage)
	default:
		state.code = mergeExitCode(state.code, codeTransport)
	}
}

func addRenderError(state *runState, ctx context.Context, err error) {
	if ctx.Err() != nil {
		state.code = mergeExitCode(state.code, codeCanceled)
		state.result.Errors = append(state.result.Errors, "canceled")
		return
	}
	state.result.Errors = append(state.result.Errors, err.Error())
	state.code = mergeExitCode(state.code, codeTransport)
}

func finishResult(ctx context.Context, o Outgoing, state runState, stdout, stderr io.Writer) int {
	if state.rendered {
		return state.code
	}
	if err := output.Render(state.result, o.Output, stdout, stderr); err != nil {
		addRenderError(&state, ctx, err)
		fmt.Fprintf(stderr, "req: writing output: %v\n", err)
	}
	return state.code
}

// dispatch is the shared send prologue: it builds the client, opens the
// request body and sends, then prints the status line. A nil response means
// the exchange never completed; the returned code is the exit code (2 for an
// unopenable body, 3 for a transport failure, 130 for cancellation).
func dispatch(ctx context.Context, o Outgoing, stderr io.Writer) (*httpclient.Response, int, error) {
	client := httpclient.Client(httpclient.Options{
		Timeout:         o.Timeout,
		InsecureTLS:     o.InsecureTLS,
		FollowRedirects: o.FollowRedirects,
	})
	headers := http.Header{}
	for _, kv := range o.Headers {
		headers.Add(kv[0], kv[1])
	}
	body, err := o.Body.Open(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "req: opening body: %v\n", err)
		if ctx.Err() != nil {
			return nil, codeCanceled, err
		}
		return nil, codeUsage, err
	}
	if body != nil {
		defer body.Close()
	}
	resp, err := httpclient.Send(ctx, client, o.Method, o.URL, body, headers)
	if err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(stderr, "req: canceled")
			return nil, codeCanceled, err
		}
		fmt.Fprintf(stderr, "req: %v\n", err)
		return nil, codeTransport, err
	}
	fmt.Fprintf(stderr, "%s %s -> %d %s in %s\n",
		o.Method, o.URL, resp.StatusCode, http.StatusText(resp.StatusCode), resp.Duration.Truncate(time.Microsecond))
	return resp, codeSuccess, nil
}

// mergeExitCode applies the public precedence: 130 > 7 > 5 > 6 > 3 > 4.
func mergeExitCode(current, candidate int) int {
	if current == codeUsage || candidate == codeUsage {
		if current == codeUsage {
			return current
		}
		return candidate
	}
	rank := func(code int) int {
		switch code {
		case codeCanceled:
			return 6
		case codeStorage:
			return 5
		case codeScript:
			return 4
		case codeAssertions:
			return 3
		case codeTransport:
			return 2
		case codeHTTPFail:
			return 1
		default:
			return 0
		}
	}
	if rank(candidate) > rank(current) {
		return candidate
	}
	return current
}
