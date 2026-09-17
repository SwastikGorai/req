# Phase 14 — Imported Postman scripts

**Goal of this phase (plain English):** Execute imported pre/post scripts with the supported pm APIs.

**Dependencies:** Phase 13 checkpoint must be `[x] Done`.

## Do this now

1. [x] Map prerequest and test events into native ordered script arrays.
2. [x] Preserve source lines, enabled state and import provenance.
3. [x] Detect obvious unsupported API usage without claiming complete static analysis.
4. [x] Run imported callback/Promise login fixtures through real saved-request execution.
5. [x] Add script compatibility table and runtime diagnostic fixture.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 14

- **Status:** `[x] Done`
- **What was actually done:** Reused the Phase 13 native script arrays and `execution.InheritedScripts` walker. Imported `prerequest`/`test` events preserve joined source lines, IDs, enabled state and provenance; enabled scripts run collection → outer folder → inner folder → request. The saved-request path now uses imported provenance as the Goja source label, so runtime compatibility failures include the Postman source path. Added a real imported login fixture exercising callback and Promise `pm.sendRequest`, with proof that import performs no HTTP and execution performs both auxiliary requests before the main request. Added a runtime diagnostic fixture and strict no-write coverage for obvious unsupported APIs.
- **Changed files:** `internal/execution/lifecycle.go`, `internal/importer/postman_test.go`, `internal/importer/testdata/postman-login.json`, `internal/importer/testdata/postman-unsupported-runtime.json`, `internal/cli/postman_scripts_test.go`, `docs/compatibility.md`, `docs/decisions.md`, `req-iterative-plan/PLAN.md`, `req-iterative-plan/phases/index.md`, `req-iterative-plan/phases/phase-14-postman-scripts.md`, and `req-iterative-plan/handoff.md`.
- **Verification:**
  - `rtk powershell -NoProfile -Command '$env:GOCACHE = (Join-Path (Get-Location) ".gocache-phase14"); rtk go test -count=1 -timeout=120s ./internal/importer'` — 13 passed.
  - `rtk powershell -NoProfile -Command '$env:GOCACHE = (Join-Path (Get-Location) ".gocache-phase14"); rtk go test -count=1 -timeout=120s ./internal/cli -run TestPostman'` — 5 passed.
  - `rtk powershell -NoProfile -Command '$env:GOCACHE = (Join-Path (Get-Location) ".gocache-phase14"); rtk go test -count=1 -timeout=120s ./internal/cli -run TestUnsupportedRuntimeAPI'` — 1 passed.
  - `rtk powershell -NoProfile -Command '$env:GOCACHE = (Join-Path (Get-Location) ".gocache-phase14"); rtk go test -count=1 -timeout=120s ./internal/cli -run TestPostmanStrictUnsupportedScriptNoWrite'` — 1 passed.
  - `rtk powershell -NoProfile -Command '$env:GOCACHE = (Join-Path (Get-Location) ".gocache-phase14"); rtk go test -count=1 -timeout=120s ./...'` — 239 passed in 9 packages.
  - `rtk powershell -NoProfile -Command '$env:GOCACHE = (Join-Path (Get-Location) ".gocache-phase14"); rtk go test -race -count=1 -timeout=180s ./...'` — 239 passed in 9 packages.
  - `rtk powershell -NoProfile -Command '$env:GOCACHE = (Join-Path (Get-Location) ".gocache-phase14"); rtk go vet ./...'` — exit 0.
  - `rtk git diff --check` and `rtk gofmt -l internal cmd` — exit 0 with no output.
- **Evidence:** `TestPostmanScriptOrder` verifies event mapping, newline source preservation, disabled filtering, provenance and inherited order. `TestPostmanImportedLogin` verifies no HTTP during import, callback/Promise auxiliary requests, variable/header mutation, main request and ordered post tests. `TestUnsupportedRuntimeAPI` verifies lenient warning plus runtime guard source location; `TestPostmanStrictUnsupportedScriptNoWrite` verifies strict rejection leaves the workspace empty.
- **Next step:** Phase 15, task 1: [phase-15-curl-import.md](phase-15-curl-import.md)
- **Resume cursor if interrupted:** Phase 15 task 1 — inspect the existing CLI parser/store seams before implementing cURL import; do not add cURL work to this phase.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

- `listen=test` maps to post-response; string-array `exec` joins with newline. The existing owner-loop engine runs imported source without a second importer-specific runtime.
- Import never executes scripts. Obvious unsupported calls are warnings only in lenient mode; runtime guards/reference errors remain authoritative for dynamically accessed unsupported APIs. Strict mode rejects any such warning before persistence.
- Provenance source/path labels runtime diagnostics; native scripts without provenance continue to use their IDs.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestPostmanScriptOrder` — imported hierarchy matches native hierarchy.
- `TestPostmanImportedLogin` — token extraction and assertions work.
- `TestUnsupportedRuntimeAPI` — actionable phase/source error.
