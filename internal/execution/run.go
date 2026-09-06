package execution

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"req/internal/httpclient"
)

// Exit codes returned by Execute, matching the CLI's contract.
const (
	codeSuccess   = 0
	codeTransport = 3
	codeHTTPFail  = 4
	codeCanceled  = 130
)

// Execute sends o and renders the result exactly like `req send`: the status
// line goes to stderr, the response body to stdout. A body-read error or a
// transport error exits 3, a canceled context exits 130, and
// FailOnHTTPError turns a status >= 400 into exit 4.
func Execute(ctx context.Context, o Outgoing, stdout, stderr io.Writer) int {
	client := httpclient.Client(httpclient.Options{
		Timeout:         o.Timeout,
		InsecureTLS:     o.InsecureTLS,
		FollowRedirects: o.FollowRedirects,
	})
	headers := http.Header{}
	for _, kv := range o.Headers {
		headers.Add(kv[0], kv[1])
	}
	var body io.Reader
	if o.Body != nil {
		body = bytes.NewReader(o.Body)
	}
	resp, err := httpclient.Send(ctx, client, o.Method, o.URL, body, headers)
	if err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(stderr, "req: canceled")
			return codeCanceled
		}
		fmt.Fprintf(stderr, "req: %v\n", err)
		return codeTransport
	}
	defer resp.Body.Close()

	fmt.Fprintf(stderr, "%s %s -> %d %s in %s\n",
		o.Method, o.URL, resp.StatusCode, http.StatusText(resp.StatusCode), resp.Duration.Truncate(time.Microsecond))
	if _, err := io.Copy(stdout, resp.Body); err != nil {
		fmt.Fprintf(stderr, "req: reading body: %v\n", err)
		return codeTransport
	}
	if o.FailOnHTTPError && resp.StatusCode >= http.StatusBadRequest {
		return codeHTTPFail
	}
	return codeSuccess
}
