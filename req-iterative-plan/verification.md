# Implementation and plan verification — 2026-09-12

The clean starting tree contains implementations for Phases 0–5. Reviewed all phase checklists against IMPLEMENTATION.md and the existing CLI, model, store, execution and scripting boundaries. Later-phase test names are acceptance targets, not existing evidence.

## Baseline checks

- `rtk go test -count=1 ./...`: exit 0, 78 tests passed across 7 packages (including packages without tests).
- `rtk go vet ./...` and `rtk go build ./...`: exit 0.
- `rtk go test -race -count=1 ./...`: exit 0, all 78 baseline tests passed.
- After prerequisite corrections: `rtk go test -count=1 ./...`: exit 0, 82 tests passed. A first run exposed changed future-schema conflict precedence; moving the name check after the revision check restored the existing contract.

## Prerequisite corrections

1. Phase 4's shared Prepare rejects placeholders in most fields but misses query keys/values. JSON bodies loaded from disk are not validated at the final execution boundary. Add regression coverage and fix these before continuing.
2. Phase 3 collection decoding and Phase 5 editing use Decoder.More outside an array/object; trailing closing delimiters can escape rejection. Require a second decode to return EOF.
3. Collection-name uniqueness is checked before the mutation lock in create/rename. Enforce it inside SaveCollection's locked write boundary so concurrent independent IDs cannot acquire the same name.
4. Editor conflicts remove the temporary edit even if recovery failed. Keep and name the temporary file when no recovery exists.
5. The shell-free editor currently uses whitespace splitting. Quoted executable paths/arguments need parsing before environment editing reuses this flow.

## Plan corrections and continuation

- Removed stale statements that no code or feasibility work exists. Historical checkpoints remain historical; this document records the new audit.
- Phase 6 must refine environment schema, safe filenames, strict revision-checked storage, editor reuse, shared send/run flags and final interpolation against the existing code before implementation.
- Phase 7 must replace the current byte-buffer body boundary when adding streaming files; preserve empty/absent bodies and cancellation cleanup.
- Phase 8 reuses existing ordered script models and the runtime owner loop. Main response limits and cancellation are prerequisites to response APIs, not work that can wait until Phase 18.
- Phases 9–19 otherwise retain their dependency order and full specification scope. Postman and cURL compatibility checks against primary sources remain implementation gates for those phases.
- The historical handoff's named `subagent-implementer` is unavailable here. Current session instructions control execution; no custom agent or historical model is assumed available.

All five prerequisite corrections above are implemented. Regression tests cover unresolved queries/invalid JSON, trailing delimiters, concurrent collection names and quoted editor arguments. Conflict handling now keeps the temporary file if recovery is unavailable. Phase 6 follows; no later phase is claimed complete by this review.

## Phase 6 continuation and final verification

After the audit fixes and plan corrections passed, implemented Phase 6: versioned environment CRUD, shared editor recovery, scoped one-pass variables, explicit process references, inherited basic/bearer/none authentication and shared send/run flags. Request creation saves auth references without resolving them. PLAN.md, the manifest and handoff now point to Phase 7; Phases 7–19 remain incomplete.

- `rtk go test -count=1 ./...`: exit 0, 89 tests passed across 8 packages.
- `rtk go vet ./...`: exit 0.
- `rtk go build ./...`: exit 0.
- `rtk proxy gofmt -l internal cmd`: exit 0, no paths printed.
- `rtk git diff --check`: exit 0, no errors.
- `rtk go test -race -count=1 ./...`: exit 0, all 89 tests passed.

All checks ran on Windows with loopback servers and temporary workspaces. No dependency changes or remote writes were made. New environment/variable test files and audit regression tests accompany the Phase 6 implementation commit.

## Phase 7 continuation

Started from clean Phase 6 commit 943fa71. Re-ran the 89-test baseline, refined Phase 7's LLD, then implemented file bodies and form uploads. The phase checkpoint records changed interfaces and test names. Trackers now point to Phase 8.

- `rtk go test -count=1 ./...`: exit 0, 102 tests passed, including the final empty-body redirect check.
- `rtk proxy go test -count=1 -run TestUntrustedBodyPaths -v ./internal/execution`: exit 0; symlink escape rejection ran successfully on Windows (no skip).
- Tests prove exact raw/multipart file bytes, JSON file substitution/validation, reference persistence and path bases, repeated/disabled forms, zero-network body conflicts, metadata/boundaries, 307/308 replay and cancellation. A 64-MiB sparse file remains streamed with less than 1 KiB of multipart framing buffered in the builder test.
- Final `rtk go test -race -count=1 ./...`: exit 0, 102 tests passed after the empty-body redirect fix.
- Final `rtk go vet ./...` and `rtk go build ./...`: exit 0.
- Final `rtk proxy gofmt -l internal cmd` and `rtk git diff --check`: exit 0, no output.

No dependencies or remote writes were added during Phase 7. JSON file inputs remain buffered for validation; raw and multipart attachment contents stream. These changes accompany the Phase 7 implementation commit.

## Post-Phase-7 review corrections

Fixed revision checks across delete confirmation, stale script callback accounting, response-stream cancellation exit codes, Host header overrides and collection filename/ID validation. Removed redundant name scans and replaced handwritten slice equality with `slices.Equal`.

- Five regression tests cover concurrent deletion edits, late callbacks, cancellation during response output, direct/saved Host overrides and mismatched collection IDs.
- `rtk go test -count=1 ./...` and `rtk go test -race -count=1 ./...`: exit 0, 107 tests passed across 8 packages in each run.
- `rtk go vet ./...` and `rtk go build ./...`: exit 0.
- `rtk proxy gofmt -l internal cmd` and `rtk git diff --check`: exit 0, no output.

These review corrections are staged separately from Phase 7 commit 93bc40c; Phase 8 remains next.

## Phase 8 — 2026-09-13

Started from clean tree at f31145e. Refined the Phase 8 LLD against ResolvePath, model.Scripts, the Phase 1 owner loop and Prepare/Execute, then delegated implementation to a subagent-implementer (authorized by the user for this session) and integrated: the review pass fixed CRLF line endings the subagent introduced in nine modified files (gofmt -w) and closed a parser gap where a separator line inside brand-new script content would have been stored as source text that breaks the next edit (now rejected with "unexpected script separator"; regression case added to TestScriptEditHappyPath).

- `gofmt -l internal cmd`: no output (after the normalization).
- `go vet ./...` and `go build ./...`: exit 0.
- `go test -count=1 ./...`: exit 0, 126 test functions pass across 7 packages (105 at Phase 7 HEAD + 21 new; earlier "107" counts included subtests).
- `go test -race -count=1 ./...`: exit 0.
- `git diff --check`: clean.
- Phase 8 coverage: inherited ordering across collection/outer/inner folder/request in both phases with logs asserted around the status line, pre-error-no-send, skip (incl. caught-sentinel and post-phase rejection), post-error-still-prints-body, transport failure without post scripts, HTTP 404 with post scripts, --fail and script-error precedence (5 over 4), per-entry deadline with a mechanical hang guard, --no-scripts, 10 MiB body limit without partial output, disabled entries filtered, and the full script-edit command surface (create, round-trip with separator preservation, damaged marker recovery, unchanged, folder/collection targets, run integration).

All work is uncommitted in the working tree; no remote action. The Phase 8 checkpoint records deviations: constant body limit, uncapped console until Phase 9, and no --no-scripts/--script-timeout on send (direct sends have no scripts).
