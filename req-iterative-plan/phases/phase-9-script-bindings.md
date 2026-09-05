# Phase 9 — Variables, request mutation and response APIs

**Goal of this phase (plain English):** Support timestamps, auth headers and response token extraction.

**Dependencies:** Phase 8 checkpoint must be `[x] Done`.

## Do this now

1. [ ] Bind scoped variable get/set/has/unset and replaceIn.
2. [ ] Bind pre-request header mutation and raw body access.
3. [ ] Expose response code/status/headers/text/json/time only after response.
4. [ ] Add bounded console logging and clear unknown-API errors.
5. [ ] Test token extraction and mutation without changing the saved definition.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 9

- **Status:** `[ ] Not started`
- **What was actually done:** Not implemented. Fill in changed files, decisions and deviations when work occurs.
- **Verification:** `go test ./internal/scripting` plus the named test cases below; add affected CLI integration tests. Record the actual test names if refined. 
- **Evidence:** Not run. Record command, exit status and observed assertions here.
- **Next step:** Phase 10, task 1: [phase-10-assertions.md](phase-10-assertions.md)
- **Resume cursor if interrupted:** Task 1; replace with exact task/test/file before handing off.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

- internal/scripting/bindings.go exposes only the supported pm surface in specification section 8.
- Header upsert replaces all case-insensitive matches. Post-phase request adapter is read-only.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestPreHeaderMutation` — generated header reaches server.
- `TestPostTokenExtraction` — environment overlay receives token.
- `TestResponseUnavailableBeforeSend` — clear script error.
