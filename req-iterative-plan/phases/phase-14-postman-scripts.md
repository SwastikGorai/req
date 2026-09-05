# Phase 14 — Imported Postman scripts

**Goal of this phase (plain English):** Execute imported pre/post scripts with the supported pm APIs.

**Dependencies:** Phase 13 checkpoint must be `[x] Done`.

## Do this now

1. [ ] Map prerequest and test events into native ordered script arrays.
2. [ ] Preserve source lines, enabled state and import provenance.
3. [ ] Detect obvious unsupported API usage without claiming complete static analysis.
4. [ ] Run imported callback/Promise login fixtures through real saved-request execution.
5. [ ] Add script compatibility table and runtime diagnostic fixture.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 14

- **Status:** `[ ] Not started`
- **What was actually done:** Not implemented. Fill in changed files, decisions and deviations when work occurs.
- **Verification:** `go test ./internal/importer` plus the named test cases below; add affected CLI integration tests. Record the actual test names if refined. 
- **Evidence:** Not run. Record command, exit status and observed assertions here.
- **Next step:** Phase 15, task 1: [phase-15-curl-import.md](phase-15-curl-import.md)
- **Resume cursor if interrupted:** Task 1; replace with exact task/test/file before handing off.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

- listen=test maps to post-response; string-array exec joins with newline.
- Import never executes scripts; runtime remains authoritative for dynamically accessed unsupported APIs.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestPostmanScriptOrder` — imported hierarchy matches native hierarchy.
- `TestPostmanImportedLogin` — token extraction and assertions work.
- `TestUnsupportedRuntimeAPI` — actionable phase/source error.
