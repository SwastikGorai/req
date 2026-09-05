# Phase 5 — Editing and organizing requests

**Goal of this phase (plain English):** Edit, rename, move and delete definitions without losing concurrent changes.

**Dependencies:** Phase 4 checkpoint must be `[x] Done`.

## Do this now

1. [ ] Implement rename and same-collection move with descendant-cycle checks.
2. [ ] Implement request deletion and confirmation for nonempty folder/collection deletion.
3. [ ] Launch configured editor without shell evaluation and validate the result.
4. [ ] Preserve invalid edits in a recovery file and detect stale revision on save.
5. [ ] Test move cycles, noninteractive deletion and editor conflicts.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 5

- **Status:** `[ ] Not started`
- **What was actually done:** Not implemented. Fill in changed files, decisions and deviations when work occurs.
- **Verification:** `go test ./internal/store` plus the named test cases below; add affected CLI integration tests. Record the actual test names if refined. 
- **Evidence:** Not run. Record command, exit status and observed assertions here.
- **Next step:** Phase 6, task 1: [phase-6-variables-auth.md](phase-6-variables-auth.md)
- **Resume cursor if interrupted:** Task 1; replace with exact task/test/file before handing off.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

- Touch internal/cli/edit.go and internal/store/mutations.go. Cross-collection move is deferred.
- Use an injected editor runner in tests; never open a real interactive editor in automated verification.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestMoveCycleRejected` — folder cannot move into itself.
- `TestEditorConflictRecovery` — newer saved file and recovery edit both survive.
- `TestDeleteNoninteractive` — missing --yes prevents destructive folder deletion.
