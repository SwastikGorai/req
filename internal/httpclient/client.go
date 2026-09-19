// Package httpclient builds and executes HTTP requests. It knows nothing
// about scripts, storage or output formatting.
package httpclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
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
	StatusCode    int
	Headers       http.Header
	HeaderEntries [][2]string
	Body          io.ReadCloser
	Duration      time.Duration
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
	if stream, ok := body.(*bodyReader); ok {
		req.ContentLength = stream.source.ContentLength
		req.GetBody = func() (io.ReadCloser, error) { return stream.source.Open(ctx) }
		if req.ContentLength == 0 {
			req.Body = http.NoBody
			req.GetBody = func() (io.ReadCloser, error) { return http.NoBody, nil }
		}
	}
	for name, values := range headers {
		if strings.EqualFold(name, "Host") && len(values) > 0 {
			req.Host = values[0]
			continue
		}
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(resp.Header))
	for name := range resp.Header {
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([][2]string, 0, len(resp.Header))
	for _, name := range names {
		for _, value := range resp.Header[name] {
			entries = append(entries, [2]string{name, value})
		}
	}
	return &Response{
		StatusCode:    resp.StatusCode,
		Headers:       resp.Header,
		HeaderEntries: entries,
		Body:          resp.Body,
		Duration:      time.Since(start),
	}, nil
}
