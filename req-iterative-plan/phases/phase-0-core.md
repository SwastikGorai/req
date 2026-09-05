# Phase 0 — Core / Walking Skeleton

**Goal of this phase (plain English):** Send one GET request and print its body without needing a workspace.

**Dependencies:** None — start here.

## Do this now

1. [x] Inspect repository instructions and existing edits; establish the Go module without overwriting user work.
2. [x] Create cmd/req/main.go and internal/cli/root.go with an injectable Run entry point.
3. [x] Implement internal/httpclient/client.go for GET using context and net/http.
4. [x] Connect req send GET URL to the executor; body goes to stdout and errors to stderr.
5. [x] Add a loopback test proving the real CLI adapter sends a GET and prints the exact response; build the binary.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 0

- **Status:** `[x] Complete`
- **What was actually done:** Repository was empty except `req-iterative-plan`; Go module `req` (Go 1.25, no third-party dependencies) established at the repository root. Created `cmd/req/main.go` (signal.NotifyContext for Ctrl+C, exits with `cli.Run`'s code), `internal/cli/root.go` (injectable `Run(ctx, args, stdout, stderr) int`; strict positional `req send METHOD URL`; unknown command/extra args/bad URL exit 2; success prints `METHOD url -> code text in duration` to stderr and streams the body to stdout; canceled context exits 130; body-read failure exits 3), `internal/httpclient/client.go` (`Response`/`Send` exactly per LLD; caller closes Body; `DefaultClient` with 30s timeout, max 10 redirects, http/https only, no retries). Tests added: `TestCoreGETBody`, `TestCoreTransportError`, `TestCoreNoWorkspace`, plus `TestSendUsageErrors` (internal/cli) and `TestSendGET`, `TestSendRejectsUnsupportedScheme` (internal/httpclient).
- **Verification:** `go vet ./...`, `go test ./internal/cli ./internal/httpclient`, `go test -count=1 -run "TestCore|TestSend" -v ./internal/cli ./internal/httpclient`, `go build ./cmd/req`.
- **Evidence:** All four commands exit 0. `go test`: `ok req/internal/cli`, `ok req/internal/httpclient`. Verbose uncached run shows all six tests PASS (TestCoreGETBody, TestCoreTransportError, TestCoreNoWorkspace, TestSendUsageErrors, TestSendGET, TestSendRejectsUnsupportedScheme). Build produced `req.exe`. Manual smoke of the real binary: `./req.exe send GET http://127.0.0.1:8111/` → stderr `GET http://127.0.0.1:8111/ -> 200 OK in 65.572ms`, stdout `smoke-payload`, exit 0; closed port → exit 3, diagnostic on stderr, empty stdout; `./req.exe send GET` → exit 2, usage on stderr. CLI tests invoke the actual `cli.Run` adapter against `httptest.NewServer` as required. Decisions recorded in [docs/decisions.md](../../docs/decisions.md).
- **Next step:** Phase 1, task 1: [phase-1-runtime-spike.md](phase-1-runtime-spike.md)
- **Resume cursor if interrupted:** Phase 0 complete; start Phase 1 task 1 (Goja feasibility spike) against the current working tree.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Initial contracts are specified below.*

- cmd/req/main.go calls cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr) and exits with its returned code.
- internal/cli/root.go: func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int. Keep argument parsing replaceable.
- internal/httpclient/client.go: type Response struct { StatusCode int; Headers http.Header; Body io.ReadCloser; Duration time.Duration }; func Send(ctx context.Context, client *http.Client, method, url string, body io.Reader, headers http.Header) (*Response, error).
- Caller owns Response.Body and always closes it; a transport error returns no usable response. Default client timeout is 30 seconds.
- Test invokes cli.Run against httptest.NewServer; no public host, auth, saved data or scripting is required.
- Use only net/http plus the selected argument parser; do not create a store or JS engine in this phase.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestCoreGETBody` — local server returns hello; stdout equals hello and exit is 0.
- `TestCoreTransportError` — connection failure writes stderr and exits 3.
- `TestCoreNoWorkspace` — request succeeds without .req.
