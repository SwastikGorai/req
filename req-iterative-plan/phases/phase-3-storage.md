# Phase 3 — Workspace and atomic persistence

**Goal of this phase (plain English):** Save a validated collection file and reload it safely.

**Dependencies:** Phase 2 checkpoint must be `[x] Done`.

## Do this now

1. [ ] Create versioned model structs and one synthetic collection fixture.
2. [ ] Implement req init and closest-ancestor workspace discovery.
3. [ ] Implement load/validate and atomic same-directory file replacement.
4. [ ] Add workspace mutation lock plus expected-content-hash conflict check.
5. [ ] Test malformed files, future versions, round trips and conflicting writers.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 3

- **Status:** `[ ] Not started`
- **What was actually done:** Not implemented. Fill in changed files, decisions and deviations when work occurs.
- **Verification:** `go test ./internal/store` plus the named test cases below; add affected CLI integration tests. Record the actual test names if refined. 
- **Evidence:** Not run. Record command, exit status and observed assertions here.
- **Next step:** Phase 4, task 1: [phase-4-saved-requests.md](phase-4-saved-requests.md)
- **Resume cursor if interrupted:** Task 1; replace with exact task/test/file before handing off.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

- internal/model/types.go owns Collection, Item, Request, Body and Script types from IMPLEMENTATION.md section 4.
- internal/store/store.go: LoadCollection(ctx context.Context, id string) (Collection, Revision, error); SaveCollection(ctx context.Context, c Collection, expected Revision) error. Revision is an opaque content hash.
- Lock covers reread, comparison and rename. Tests must verify the check and write cannot race.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestStoreRoundTrip` — stable IDs survive reload.
- `TestStoreConflict` — stale revision cannot overwrite.
- `TestStoreFutureSchema` — unknown schema is not rewritten.
