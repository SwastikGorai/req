# Phase 19 — Integrated acceptance and handoff

**Goal of this phase (plain English):** Verify the full workflow and deliver reproducible instructions.

**Dependencies:** Phase 18 checkpoint must be `[x] Done`.

## Do this now

1. [x] Build the loopback acceptance fixture and automate native login/profile workflow.
2. [x] Run imported-script and cURL round-trip acceptance through CLI adapters.
3. [x] Finish README, compatibility matrix, examples and source-linked runtime decision.
4. [x] Run formatting, vet, tests, race checks and platform cross-builds; record actual results.
5. [x] Update all checkpoints and final handoff with limitations and next actions.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 19

- **Status:** `[x] Done`
- **What was actually done:** Added `internal/cli/acceptance_test.go` with
  integrated loopback workflows for native persisted-token login/profile,
  imported Postman callback/Promise/inherited scripts, and cURL
  create/export/import/run semantics. Added a polished root README plus focused
  getting-started, CLI-reference, scripting and import/export documentation,
  along with Phase 19 compatibility/decision records. No production code or
  remote/deployment scaffolding was added.
- **Verification:** `rtk gofmt -l .` returned no paths; `rtk go vet ./...`,
  `rtk git diff --check`, and `rtk go build ./cmd/req` exited 0. The focused
  command `rtk go test -count=1 ./internal/cli -run
  'TestAcceptance(NativeLogin|ImportedScripts|Curl)$'` passed all 3 named
  tests. `rtk go test -count=1 -timeout=180s ./...` passed 297 tests in 11
  packages. `rtk go test -race -count=1 -timeout=240s ./...` passed the same
  297 tests. Cross-builds passed for `windows/amd64`, `linux/amd64`,
  `darwin/amd64` and `darwin/arm64`; those are compile-only checks, not target
  runtime tests. Cross-build artifacts were removed from a generated temp
  directory outside the repository.
- **Evidence:** The acceptance tests assert the persisted Authorization token
  on a separate run, zero HTTP traffic during Postman import, callback and
  Promise auxiliary requests plus inherited headers/order, and method/query/
  header/body semantics after cURL export/import. README commands match the
  implemented CLI help. Goja runtime links point to the primary repository and
  API documentation; the docs explicitly avoid a full sandbox claim.
- **Limitations:** This remains a documented Postman/cURL compatibility
  subset. Unsupported Postman/Node APIs, shell constructs and cURL flags are
  rejected or warned; no GUI, cloud sync, collection runner, cookie jar,
  OAuth UI, remote publish or deployment workflow exists. Script/runtime,
  body/log redaction is not promised, and Goja is not a hard heap/security
  sandbox. Cross-build success does not establish platform runtime support.
- **Next step:** Parent integrator may review and commit the Phase 19 changes;
  no remote action is authorized or required.
- **Resume cursor if interrupted:** Final review only; all Phase 19 tasks and
  gates above are complete.

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
