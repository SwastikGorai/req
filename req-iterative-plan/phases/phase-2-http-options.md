# Phase 2 — HTTP methods, bodies and failures

**Goal of this phase (plain English):** Make direct requests useful for ordinary API debugging.

**Dependencies:** Phase 1 checkpoint must be `[x] Done`.

## Do this now

1. [ ] Add method, repeated header/query flags and raw or inline JSON body.
2. [ ] Validate http/https, conflicting body modes and final JSON before sending.
3. [ ] Add timeout, TLS verification, redirects, --insecure and --no-follow.
4. [ ] Implement --fail and verify stdout/stderr separation for HTTP and transport errors.
5. [ ] Add local-server tests for encoding, headers, JSON and redirect auth handling.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 2

- **Status:** `[ ] Not started`
- **What was actually done:** Not implemented. Fill in changed files, decisions and deviations when work occurs.
- **Verification:** `go test ./internal/httpclient` plus the named test cases below; add affected CLI integration tests. Record the actual test names if refined. 
- **Evidence:** Not run. Record command, exit status and observed assertions here.
- **Next step:** Phase 3, task 1: [phase-3-storage.md](phase-3-storage.md)
- **Resume cursor if interrupted:** Task 1; replace with exact task/test/file before handing off.

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
