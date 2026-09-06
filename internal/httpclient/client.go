// Package httpclient builds and executes HTTP requests. It knows nothing
// about scripts, storage or output formatting.
package httpclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultTimeout bounds the complete request/response exchange, including
// reading the body.
const DefaultTimeout = 30 * time.Second

// maxRedirects is the redirect ceiling from IMPLEMENTATION.md section 6.
const maxRedirects = 10

// Response is one received HTTP exchange. The caller owns Body and must close
// it on every path; a transport error returns no usable Response.
type Response struct {
	StatusCode int
	Headers    http.Header
	Body       io.ReadCloser
	Duration   time.Duration
}

// DefaultClient returns the process-wide default policy: 30s deadline, at
// most maxRedirects redirects, TLS verification on, no automatic retries.
func DefaultClient() *http.Client {
	return Client(Options{})
}

// Send executes one request and measures elapsed time to the response
// headers. Only http and https URLs are allowed.
func Send(ctx context.Context, client *http.Client, method, url string, body io.Reader, headers http.Header) (*Response, error) {
	if client == nil {
		client = DefaultClient()
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme %q: only http and https are allowed", req.URL.Scheme)
	}
	for name, values := range headers {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	return &Response{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
		Body:       resp.Body,
		Duration:   time.Since(start),
	}, nil
}
