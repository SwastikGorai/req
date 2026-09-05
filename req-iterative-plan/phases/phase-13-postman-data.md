# Phase 13 — Postman collection and environment import

**Goal of this phase (plain English):** Import nested request data with explicit compatibility warnings.

**Dependencies:** Phase 12 checkpoint must be `[x] Done`.

## Do this now

1. [ ] Add synthetic v2.1 fixtures and normalize URL string/object forms.
2. [ ] Convert folders, requests, disabled entries, variables, auth and body modes.
3. [ ] Implement environment import and deterministic collision handling.
4. [ ] Add structured warnings, strict validation and unsupported-auth execution blocking.
5. [ ] Test malformed input, duplicate query representation and no-write strict failure.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 13

- **Status:** `[ ] Not started`
- **What was actually done:** Not implemented. Fill in changed files, decisions and deviations when work occurs.
- **Verification:** `go test ./internal/importer` plus the named test cases below; add affected CLI integration tests. Record the actual test names if refined. 
- **Evidence:** Not run. Record command, exit status and observed assertions here.
- **Next step:** Phase 14, task 1: [phase-14-postman-scripts.md](phase-14-postman-scripts.md)
- **Resume cursor if interrupted:** Task 1; replace with exact task/test/file before handing off.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

- internal/importer/postman.go normalizes before calling store; do not write partially converted collections.
- Known unsupported source remains recoverable in metadata; collection schema v2.1 is the target.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestPostmanNestedData` — tree structure survives.
- `TestPostmanStrictNoWrite` — lossy input leaves store unchanged.
- `TestPostmanUnsupportedAuthBlocked` — no silent unauthenticated send.
