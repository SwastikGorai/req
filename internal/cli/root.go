// Package cli parses arguments, invokes services and maps results to exit
// codes. stdout carries response bodies; stderr carries diagnostics.
package cli

import (
	"context"
	"fmt"
	"io"
)

// Exit codes from IMPLEMENTATION.md section 6; later phases add the rest.
const (
	exitSuccess   = 0
	exitUsage     = 2
	exitTransport = 3
	exitHTTPFail  = 4
	exitCanceled  = 130
)

// Version is reported by req --version.
const Version = "0.0.1"

// Run executes one command and returns the process exit code.
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

func printUsage(w io.Writer, code int) int {
	fmt.Fprintf(w, `req — local-first HTTP client

Usage:
  req send METHOD URL [flags]
                          send one request; the body goes to stdout, status
                          and elapsed time to stderr

Send flags:
  -H, --header NAME:VALUE   header entry; repeat to append
  --query KEY=VALUE         query entry; repeat to append
  --method, --url           instead of positional METHOD / URL
  --body TEXT               raw body (Content-Type text/plain)
  --json JSON               JSON body, validated before sending
                            (--body and --json are mutually exclusive; an
                            explicit Content-Type header wins)
  --timeout DURATION        request deadline (default 30s)
  --no-follow               return 3xx responses instead of following them
  --insecure                skip TLS certificate verification
  --fail                    exit 4 when the response status is >= 400

  req --version
  req help

Workspace, environment and scripting commands arrive with later phases.
`)
	return code
}
