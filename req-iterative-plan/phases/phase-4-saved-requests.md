# Phase 4 — Collections, folders and saved requests

**Goal of this phase (plain English):** Create a nested request, restart the CLI and execute it.

**Dependencies:** Phase 3 checkpoint must be `[x] Done`.

## Do this now

1. [ ] Implement collection create/list and folder create with explicit --parents.
2. [ ] Implement validated path lookup and request create/show/list.
3. [ ] Add tree output with stable sibling order.
4. [ ] Connect req run PATH to the same execution path as direct send.
5. [ ] Test fresh-process persistence and duplicate-name rejection.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 4

- **Status:** `[ ] Not started`
- **What was actually done:** Not implemented. Fill in changed files, decisions and deviations when work occurs.
- **Verification:** `go test ./internal/store` plus the named test cases below; add affected CLI integration tests. Record the actual test names if refined. 
- **Evidence:** Not run. Record command, exit status and observed assertions here.
- **Next step:** Phase 5, task 1: [phase-5-editing.md](phase-5-editing.md)
- **Resume cursor if interrupted:** Task 1; replace with exact task/test/file before handing off.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

- internal/store/selectors.go resolves slash paths; names cannot contain slash, dot-only segments or empty values.
- internal/execution/run.go coordinates load and send; CLI handlers do not call one another.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestSavedRunAfterReload` — recreated CLI loads and sends saved request.
- `TestFolderDuplicateName` — duplicate sibling is rejected.
- `TestFolderParents` — missing ancestors require --parents.
