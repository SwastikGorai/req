// Package cli parses arguments, invokes services and maps results to exit
// codes. stdout carries response bodies; stderr carries diagnostics.
package cli

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"req/internal/httpclient"
)

// Exit codes from IMPLEMENTATION.md section 6; later phases add the rest.
const (
	exitSuccess   = 0
	exitUsage     = 2
	exitTransport = 3
	exitCanceled  = 130
)

// Version is reported by req --version.
const Version = "0.0.1"

// Run executes one command and returns the process exit code. Argument
// parsing is intentionally trivial and replaceable while the command set is
// small; flags arrive with later phases.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return printUsage(stderr, exitUsage)
	}
	switch args[0] {
	case "send":
		return runSend(ctx, args[1:], stdout, stderr)
	case "help", "-h", "--help":
		return printUsage(stdout, exitSuccess)
	case "--version":
		fmt.Fprintf(stdout, "req version %s\n", Version)
		return exitSuccess
	default:
		fmt.Fprintf(stderr, "req: unknown command %q\n\n", args[0])
		return printUsage(stderr, exitUsage)
	}
}

// runSend handles `req send METHOD URL`. Extra or malformed arguments fail
// with exit 2 rather than being ignored.
func runSend(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, "usage: req send METHOD URL")
		return exitUsage
	}
	method, rawURL := strings.ToUpper(args[0]), args[1]
	if u, err := url.Parse(rawURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		fmt.Fprintf(stderr, "req: URL must be absolute http or https, got %q\n", rawURL)
		return exitUsage
	}

	resp, err := httpclient.Send(ctx, httpclient.DefaultClient(), method, rawURL, nil, http.Header{})
	if err != nil {
		if ctx.Err() != nil {
			fmt.Fprintln(stderr, "req: canceled")
			return exitCanceled
		}
		fmt.Fprintf(stderr, "req: %v\n", err)
		return exitTransport
	}
	defer resp.Body.Close()

	fmt.Fprintf(stderr, "%s %s -> %d %s in %s\n",
		method, rawURL, resp.StatusCode, http.StatusText(resp.StatusCode), resp.Duration.Truncate(time.Microsecond))
	if _, err := io.Copy(stdout, resp.Body); err != nil {
		fmt.Fprintf(stderr, "req: reading body: %v\n", err)
		return exitTransport
	}
	return exitSuccess
}

func printUsage(w io.Writer, code int) int {
	fmt.Fprintf(w, `req — local-first HTTP client

Usage:
  req send METHOD URL     send one request; the body goes to stdout,
                          status and elapsed time to stderr
  req --version
  req help

Global flags --workspace, --no-color and shared request flags arrive with
later phases.
`)
	return code
}
