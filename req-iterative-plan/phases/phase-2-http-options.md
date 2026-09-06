# Phase 2 — HTTP methods, bodies and failures

**Goal of this phase (plain English):** Make direct requests useful for ordinary API debugging.

**Dependencies:** Phase 1 checkpoint must be `[x] Done`.

## Do this now

1. [x] Add method, repeated header/query flags and raw or inline JSON body.
2. [x] Validate http/https, conflicting body modes and final JSON before sending.
3. [x] Add timeout, TLS verification, redirects, --insecure and --no-follow.
4. [x] Implement --fail and verify stdout/stderr separation for HTTP and transport errors.
5. [x] Add local-server tests for encoding, headers, JSON and redirect auth handling.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 2

- **Status:** `[x] Complete`
- **What was actually done:** Refined the LLD against the Phase 0 code, then: `internal/httpclient/build.go` (new) — `Options{Timeout, InsecureTLS, FollowRedirects}` and `Client(opts)`; `DefaultClient()` now returns `Client(Options{})` and `Send` is unchanged (Phase 0 contract preserved). `internal/cli/send.go` (new) — `runSend` moved here from root.go with real flag parsing: positional `METHOD URL` or `--method`/`--url` (conflicts and duplicates exit 2), repeated `-H/--header` and `--query` (append, duplicates and empty values preserved, existing URL query bytes never re-encoded), `--body`/`--json` mutually exclusive with JSON validated before sending, `--timeout` (positive Go duration), `--no-follow` (3xx returned as-is), `--insecure`, `--fail`. Explicit headers override generated Content-Type (application/json / text/plain); empty `--body ""` sends Content-Length 0 and differs from an absent body; unknown flags never silently dropped. root.go slimmed to dispatch/usage/version; `exitHTTPFail = 4` added.
- **Deviations/refinements:** net/http only compares host *names* when deciding to forward credentials on redirects, so Go 1.25 forwards `Authorization` across ports and on an https→http downgrade to the same host. We now enforce a strict web-origin rule (scheme + host + port) in `CheckRedirect`: `Authorization`, `Proxy-Authorization`, `Cookie`, `Cookie2`, `Www-Authenticate`, `Proxy-Authenticate` are stripped when the redirect leaves the original origin, and kept on same-origin redirects. Recorded in docs/decisions.md. Go 1.25 renamed the redirect sentinel to `http.ErrUseLastResponse` (LLD-era `ErrUseOfLastResponse` no longer exists).
- **Verification:** `go test ./internal/httpclient`; `go vet ./...`; `go test -race -count=1 ./...`; `go build ./cmd/req`.
- **Evidence:** All exit 0. Named tests PASS: `TestHTTPRepeatedQuery` (server saw `tag=[a b]` plus preserved `keep=1`), `TestHTTPJSON` (exact payload bytes, Content-Type application/json, method POST), `TestHTTPFailFlag` (400 → exit 0 without `--fail`, exit 4 with it, body on stdout and status line on stderr in both), `TestHTTPRedirectSensitiveHeaders` (unrelated origin received no Authorization). Additional: `TestHTTPRedirectSameOriginKeepsHeaders`, `TestSendValidation` (17 usage cases → exit 2), `TestSendBodyModes`, `TestSendRedirectNoFollow`, `TestSendInsecureTLS` (self-signed server → exit 3 then exit 0), `TestSendTimeout` (50ms deadline → exit 3 promptly), `TestClientPolicy`, `TestSameOrigin`, `TestClientRedirectPolicy`; Phase 0/1 tests still pass. Full race suite `ok` for all three packages. Manual smoke: `--query` twice echoed as `?a=1&tag=x&tag=y`; 404 with `--fail` → exit 4; conflicting body flags → exit 2 with a clear message.
- **Next step:** Phase 3, task 1: [phase-3-storage.md](phase-3-storage.md)
- **Resume cursor if interrupted:** Phase 2 complete; start Phase 3 task 1 (workspace discovery + atomic JSON store) against the current working tree.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

- Touch internal/cli/send.go and internal/httpclient/build.go; keep the Phase 0 service callable.
- HTTP >=400 is a received response; transport failure is separate. Explicit headers override generated Content-Type/auth.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestHTTPRepeatedQuery` — duplicate query values survive.
- `TestHTTPJSON` — payload bytes and Content-Type match.
- `TestHTTPFailFlag` — 400 returns 0 or 4 with --fail.
- `TestHTTPRedirectSensitiveHeaders` — unrelated origin receives no inherited credentials.
