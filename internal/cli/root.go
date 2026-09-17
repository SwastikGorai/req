// Package cli parses arguments, invokes services and maps results to exit
// codes. stdout carries response bodies; stderr carries diagnostics.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
)

// Exit codes from IMPLEMENTATION.md section 6; later phases add the rest.
const (
	exitSuccess    = 0
	exitUsage      = 2
	exitTransport  = 3
	exitHTTPFail   = 4
	exitScript     = 5 // a pre/post script failed, or the body hit the script buffer limit
	exitAssertions = 6 // failed pm.test assertions
	exitStorage    = 7
	exitCanceled   = 130
)

// osGetwd is a variable so tests could stub it; the working directory is
// otherwise read directly.
var osGetwd = os.Getwd

// Version is reported by req --version.
const Version = "0.0.1"

// Run executes one command and returns the process exit code.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return printUsage(stderr, exitUsage)
	}
	inv, err := parseGlobals(args)
	if err != nil {
		fmt.Fprintf(stderr, "req: %v\n", err)
		return exitUsage
	}
	if len(inv.args) == 0 {
		return printUsage(stderr, exitUsage)
	}
	switch inv.args[0] {
	case "send":
		return runSend(ctx, invocation{args: inv.args[1:], workspace: inv.workspace}, stdout, stderr)
	case "env":
		return runEnv(ctx, invocation{args: inv.args[1:], workspace: inv.workspace}, stdout, stderr)
	case "init":
		return runInit(invocation{args: inv.args[1:], workspace: inv.workspace}, stdout, stderr)
	case "collection":
		return runCollection(ctx, invocation{args: inv.args[1:], workspace: inv.workspace}, stdout, stderr)
	case "folder":
		return runFolder(ctx, invocation{args: inv.args[1:], workspace: inv.workspace}, stdout, stderr)
	case "request":
		return runRequest(ctx, invocation{args: inv.args[1:], workspace: inv.workspace}, stdout, stderr)
	case "tree":
		return runTree(ctx, invocation{args: inv.args[1:], workspace: inv.workspace}, stdout, stderr)
	case "run":
		return runRun(ctx, invocation{args: inv.args[1:], workspace: inv.workspace}, stdout, stderr)
	case "script":
		return runScript(ctx, invocation{args: inv.args[1:], workspace: inv.workspace}, stdout, stderr)
	case "import":
		return runImport(ctx, invocation{args: inv.args[1:], workspace: inv.workspace}, stdout, stderr)
	case "help", "-h", "--help":
		return printUsage(stdout, exitSuccess)
	case "--version":
		fmt.Fprintf(stdout, "req version %s\n", Version)
		return exitSuccess
	default:
		fmt.Fprintf(stderr, "req: unknown command %q\n\n", inv.args[0])
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
  --body-file PATH          stream exact file bytes (application/octet-stream)
  --json JSON|@PATH         JSON text/file; substitute and validate before send
  --urlencoded KEY=VALUE    URL-encoded form field; repeat to append
  --form KEY=VALUE          multipart text field; leading @ stays literal
  --form-file KEY=PATH      multipart file attachment; repeat/mix with --form
                            Body modes are exclusive. Explicit Content-Type
                            wins; multipart requires multipart/form-data and
                            a matching boundary (generated when omitted).
                            File paths on run use the workspace root;
                            direct send paths use the current directory.
  --timeout DURATION        request deadline (default 30s)
  --no-follow               return 3xx responses instead of following them
  --insecure                skip TLS certificate verification
  --fail                    exit 4 when the response status is >= 400

  req collection create NAME                     create an empty collection
  req collection list                            list collections as NAME<TAB>ID
  req collection rename OLD NEW                  rename a collection
  req collection delete NAME [--yes]             delete a collection; a nonempty
                                                 one needs --yes or a confirm
  req folder create PATH [--parents]             create a folder in a collection
  req folder rename PATH NEW                     rename a folder
  req folder move SRC DEST                       move an item under a folder
                                                 or a collection root
  req folder delete PATH [--yes]                 delete a folder and its subtree
  req request create PATH --method M --url U     save a request; also accepts
                                                 -H, --query, all body flags,
                                                 auth flags and --parents
  req request list PATH                          list requests as
                                                 NAME<TAB>METHOD<TAB>URL
  req request show PATH                          print one saved request as JSON
  req request rename PATH NEW                    rename a saved request
  req request move SRC DEST                      move a request under a folder
                                                 or a collection root
  req request delete PATH [--yes]                delete a saved request
  req request edit PATH                          edit a saved request in
                                                 $EDITOR or $VISUAL
  req script edit PATH (--pre|--post)            edit a collection, folder or
                                                 request script phase in
                                                 $EDITOR or $VISUAL; stored
                                                 entries are separated by
                                                 // ---- req script ID ----
                                                 marker lines
  req tree PATH                                  print a collection or folder
                                                 subtree
  req run PATH [flags]                           execute a saved request;
                                                 send flags apply as overrides

  req import postman FILE [--name NAME] [--strict]
                                                 import a Postman v2.1 collection
  req import postman-env FILE [--name NAME] [--strict]
                                                 import a Postman environment

  req --version
  req help

  req env create|list|edit|delete [NAME]          manage environments

Send/run variable and auth flags:
  --env NAME                select workspace environment
  --var KEY=VALUE           highest-priority variable; repeat (last wins)
  --bearer TOKEN            bearer auth (supports {{references}})
  --basic-user USER --basic-password PASSWORD  basic auth; both required
  --no-auth                 disable inherited auth
                            Auth modes conflict; explicit Authorization wins.
  {{env:NAME}}              read one process environment variable explicitly

Run-only script flags:
  --no-scripts              skip pre-request and post-response scripts; the
                            body streams exactly like a direct send
  --script-timeout DURATION deadline per script entry, including asynchronous
                            work (default 5s)
`)
	return code
}
