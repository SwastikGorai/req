# Phase 13 — Postman collection and environment import

**Goal of this phase (plain English):** Import nested request data with explicit compatibility warnings.

**Dependencies:** Phase 12 checkpoint must be `[x] Done`.

## Do this now

1. [x] Add synthetic v2.1 fixtures and normalize URL string/object forms.
2. [x] Convert folders, requests, disabled entries, variables, auth and body modes.
3. [x] Implement environment import and deterministic collision handling.
4. [x] Add structured warnings, strict validation and unsupported-auth execution blocking.
5. [x] Test malformed input, duplicate query representation and no-write strict failure.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 13

- **Status:** `[x] Done`
- **What was actually done:** Added a side-effect-free Postman v2.1 collection/environment parser and atomic store imports. Collections retain nested folders and request order, variables (including disabled entries), supported inherited auth, native body modes, disabled items and script source/provenance. Unsupported auth/body/method data is retained as import metadata and blocks execution until explicitly resolved. Environments retain disabled variables. Lenient imports sanitize invalid names/IDs and warn; strict imports reject any lossy warning before writing. Default name collisions use deterministic suffixes, while explicit collisions return the existing duplicate error.
- **Changed files:** `internal/importer/postman.go`, `internal/importer/postman_test.go`, `internal/importer/testdata/synthetic-collection.json`, `internal/importer/testdata/synthetic-environment.json`, `internal/model/types.go`, `internal/model/environment.go`, `internal/cli/import.go`, `internal/cli/import_test.go`, `internal/cli/root.go`, `internal/cli/run.go`, `internal/cli/variables.go`, `internal/execution/lifecycle.go`, `docs/decisions.md`, `docs/compatibility.md`, `req-iterative-plan/PLAN.md`, `req-iterative-plan/phases/index.md`, and `req-iterative-plan/handoff.md`.
- **Verification:**
  - `rtk go test -count=1 -timeout=120s ./internal/importer` — 12 passed.
  - `rtk go test -count=1 -timeout=120s ./internal/cli -run 'TestPostman'` — 3 passed.
  - `rtk go test -count=1 -timeout=120s ./internal/model ./internal/execution` — 25 passed in 2 packages.
  - `rtk go test -count=1 -timeout=120s ./...` — 235 passed in 9 packages.
  - `rtk go test -race -count=1 -timeout=180s ./...` — 235 passed in 9 packages.
  - `rtk go test -race -count=10 -timeout=180s -run TestPostman ./internal/importer ./internal/cli` — 150 passed in 2 packages.
  - `rtk powershell -NoProfile -Command '$env:GOCACHE = (Join-Path (Get-Location) ".gocache-phase13"); go vet ./...'` — exit 0 (the default Go cache is read-only in this sandbox; the temporary cache was removed afterward).
  - `rtk git diff --check` and `rtk gofmt -l internal cmd` — exit 0 with no output.
- **Evidence:** `TestPostmanNestedData` covers nested tree/order, disabled values/items, auth, native body modes and script provenance; `TestPostmanStrictNoWrite`, `TestPostmanMalformedNoWrite`, and `TestPostmanStrictUnsupportedSchema` cover validation and atomic no-write behavior; collision, unsupported-auth/block/override, environment, raw-query/path-variable, file-body and deterministic-name tests cover the fixed edge cases. CLI tests cover import summaries, strict failure and execution blocking.
- **Next step:** Phase 14, task 1: [phase-14-postman-scripts.md](phase-14-postman-scripts.md)
- **Resume cursor if interrupted:** Phase 14 task 1 — inspect the stored Postman script provenance and implement only the script compatibility contract in [phase-14-postman-scripts.md](phase-14-postman-scripts.md).

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

- `internal/importer/postman.go` normalizes before calling store; parsing is side-effect-free and each successful import is one atomic store write.
- Known unsupported source remains recoverable in `ImportMetadata`; collection schema v2.1 is the target. Imported file attachments are retained as untrusted paths and never read during import.
- Disabled variables are persisted separately from active variables; run-time variable layering filters them without mutating saved data. Raw URL text wins over duplicate structured query pairs, while distinct structured pairs remain native query data.
- Imported script source is stored for the later script phase but is not executed or expanded into a Phase 14 compatibility layer here.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestPostmanNestedData` — tree structure survives.
- `TestPostmanStrictNoWrite` — lossy input leaves store unchanged.
- `TestPostmanUnsupportedAuthBlocked` — no silent unauthenticated send.

Additional fixed-contract coverage is in `internal/importer/postman_test.go` and `internal/cli/import_test.go`, including malformed documents, schema rejection, deterministic collisions/names/IDs, disabled environment values, auth override, raw-query deduplication, path variables, file-body trust marking and CLI summaries.
