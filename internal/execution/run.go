package execution

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"req/internal/httpclient"
)

// Exit codes returned by Execute, matching the CLI's contract.
const (
	codeSuccess    = 0
	codeUsage      = 2
	codeTransport  = 3
	codeHTTPFail   = 4
	codeScript     = 5 // a pre/post script failed, or the body hit the script buffer limit
	codeAssertions = 6 // failed pm.test assertions
	codeCanceled   = 130
)

// Execute sends o and renders the result exactly like `req send`: the status
// line goes to stderr, the response body to stdout. A body-read error or a
// transport error exits 3, a canceled context exits 130, and
// FailOnHTTPError turns a status >= 400 into exit 4.
func Execute(ctx context.Context, o Outgoing, stdout, stderr io.Writer) int {
	resp, code := dispatch(ctx, o, stderr)
	if resp == nil {
		return code
	}
	defer resp.Body.Close()
	if _, err := io.Copy(stdout, resp.Body); err != nil {
		fmt.Fprintf(stderr, "req: reading body: %v\n", err)
		if ctx.Err() != nil {
			return codeCanceled
		}
		return codeTransport
	}
	if o.FailOnHTTPError && resp.StatusCode >= http.StatusBadRequest {
		return codeHTTPFail
	}
	return codeSuccess
}

// dispatch is the shared send prologue: it builds the client, opens the
// request body and sends, then prints the status line. A nil response means
// the exchange never completed; the returned code is the exit code (2 for an
// unopenable body, 3 for a transport failure, 130 for cancellation).
func dispatch(ctx context.Context, o Outgoing, stderr io.Writer) (*httpclient.Response, int) {
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
			return nil, codeCanceled
		}
		return nil, codeUsage
	}
	if body != nil {
		defer body.Close()
	}
	resp, err := httpclient.Send(ctx, client, o.Method, o.URL, body, headers)
	if err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(stderr, "req: canceled")
			return nil, codeCanceled
		}
		fmt.Fprintf(stderr, "req: %v\n", err)
		return nil, codeTransport
	}
	fmt.Fprintf(stderr, "%s %s -> %d %s in %s\n",
		o.Method, o.URL, resp.StatusCode, http.StatusText(resp.StatusCode), resp.Duration.Truncate(time.Microsecond))
	return resp, codeSuccess
}
