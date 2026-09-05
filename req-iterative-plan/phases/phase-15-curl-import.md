# Phase 15 — cURL command import

**Goal of this phase (plain English):** Convert a documented subset without invoking a shell.

**Dependencies:** Phase 14 checkpoint must be `[x] Done`.

## Do this now

1. [ ] Implement POSIX tokenization for one curl command and line continuations.
2. [ ] Map method/header/URL/data/JSON flags and repeated data semantics.
3. [ ] Map auth/form/-G/redirect/TLS flags and file-reference metadata.
4. [ ] Reject active shell constructs, unsupported combinations and multiple URLs.
5. [ ] Test literal special characters, method inference and redirect defaults.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 15

- **Status:** `[ ] Not started`
- **What was actually done:** Not implemented. Fill in changed files, decisions and deviations when work occurs.
- **Verification:** `go test ./internal/importer` plus the named test cases below; add affected CLI integration tests. Record the actual test names if refined. 
- **Evidence:** Not run. Record command, exit status and observed assertions here.
- **Next step:** Phase 16, task 1: [phase-16-curl-export.md](phase-16-curl-export.md)
- **Resume cursor if interrupted:** Task 1; replace with exact task/test/file before handing off.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

- internal/importer/curl.go parses text only. Consult official cURL docs for each accepted combination.
- Do not expand shell environment variables or read imported outside-workspace files.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestCurlQuotedLiteral` — single-quoted shell characters remain data.
- `TestCurlRejectSubstitution` — active command substitution is rejected.
- `TestCurlNoLocation` — import without -L does not follow redirects.
