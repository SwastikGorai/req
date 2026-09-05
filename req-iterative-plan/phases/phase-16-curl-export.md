# Phase 16 — cURL export

**Goal of this phase (plain English):** Export saved requests with safe quoting and honest omissions.

**Dependencies:** Phase 15 checkpoint must be `[x] Done`.

## Do this now

1. [ ] Implement POSIX quoting for headers, URL and body values.
2. [ ] Preserve variable placeholders and file references by default.
3. [ ] Add explicit --resolve --env for substituted exports.
4. [ ] Warn on scripts/nonrepresentable behavior and fail in strict mode.
5. [ ] Round-trip supported native requests through export/import and local echo.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 16

- **Status:** `[ ] Not started`
- **What was actually done:** Not implemented. Fill in changed files, decisions and deviations when work occurs.
- **Verification:** `go test ./internal/exporter` plus the named test cases below; add affected CLI integration tests. Record the actual test names if refined. 
- **Evidence:** Not run. Record command, exit status and observed assertions here.
- **Next step:** Phase 17, task 1: [phase-17-variable-persistence.md](phase-17-variable-persistence.md)
- **Resume cursor if interrupted:** Task 1; replace with exact task/test/file before handing off.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

- internal/exporter/curl.go consumes definitions, not the execution lifecycle.
- Resolved exports can expose secrets; make opt-in behavior clear in help.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestCurlRoundTrip` — supported method/body/query semantics match.
- `TestCurlScriptsWarn` — scripts never execute during export.
- `TestCurlResolveOptIn` — default output retains placeholders.
