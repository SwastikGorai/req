# Phase 19 — Integrated acceptance and handoff

**Goal of this phase (plain English):** Verify the full workflow and deliver reproducible instructions.

**Dependencies:** Phase 18 checkpoint must be `[x] Done`.

## Do this now

1. [ ] Build the loopback acceptance fixture and automate native login/profile workflow.
2. [ ] Run imported-script and cURL round-trip acceptance through CLI adapters.
3. [ ] Finish README, compatibility matrix, examples and source-linked runtime decision.
4. [ ] Run formatting, vet, tests, race checks and platform cross-builds; record actual results.
5. [ ] Update all checkpoints and final handoff with limitations and next actions.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 19

- **Status:** `[ ] Not started`
- **What was actually done:** Not implemented. Fill in changed files, decisions and deviations when work occurs.
- **Verification:** `gofmt -l .` returns no source files; `go vet ./...`, `go test ./...`, `go test -race ./...`, `go build ./cmd/req`; record supported-platform cross-build results separately.
- **Evidence:** Not run. Record command, exit status and observed assertions here.
- **Next step:** Deliver verified source and final handoff; do not publish remotely without authorization.
- **Resume cursor if interrupted:** Task 1; replace with exact task/test/file before handing off.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

- Reuse specification section 11 references and full scope in IMPLEMENTATION.md.
- Cross-builds are not runtime testing on those platforms. Mandatory phases cannot be marked done if checks are blocked.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestAcceptanceNativeLogin` — persisted token authenticates subsequent CLI.
- `TestAcceptanceImportedScripts` — imported scripts run end to end.
- `TestAcceptanceCurl` — supported round trip preserves observed HTTP.
