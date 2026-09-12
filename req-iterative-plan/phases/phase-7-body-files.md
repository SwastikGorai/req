# Phase 7 — File uploads and body formats

**Goal of this phase (plain English):** Send JSON files, URL-encoded forms and multipart uploads.

**Dependencies:** Phase 6 checkpoint must be `[x] Done`.

## Do this now

1. [x] Implement --body-file and --json @file with explicit path resolution.
2. [x] Add URL-encoded repeated fields and disabled entry filtering.
3. [x] Add multipart text/file fields with streaming and cleanup.
4. [x] Validate conflicting modes and preserve empty versus absent bodies.
5. [x] Test file bytes, repeated fields and cancellation cleanup.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 7

- **Status:** `[x] Complete`
- **What was actually done:** Added `internal/cli/body.go`, `internal/execution/body.go`, `internal/httpclient/body.go` and corresponding tests. Shared all body flags across send/run/request-create; replaced execution byte bodies with replayable streamed bodies; added model empty-value/tag validation, file trust markers, path resolution, JSON file validation, form encoding, multipart metadata/boundaries, content lengths and redirect reopening. Updated help and decisions; existing fixtures now use optional inline-value pointers. No new dependency.
- **Verification:** Baseline `rtk go test -count=1 ./...` passed with 89 tests. Phase 7 full tests, race suite, vet, build, formatting and diff checks are recorded in verification.md. Named tests: TestMultipartBytes, TestBodyStreamCleanup, TestBodyMetadataValidation, TestBodyResolution, TestUntrustedBodyPaths, TestEmptyAndAbsentBody, TestBodyPathBase, TestCLIForms, TestBodyModeConflict, TestBodyRedirectReplay, TestUploadCancellation.
- **Evidence:** 102 tests pass including loopback raw/JSON/multipart uploads, repeated form fields and CLI body-mode conflicts before network access; 307/308 replay preserves exact bytes/boundaries. A sparse 64-MiB upload is streamed with only framing buffered; cancellation exits 130 and close releases file handles. Explicit Windows symlink-containment test passed. Initial compile failure was an old string-valued fixture; initial cleanup assertion assumed os.ErrClosed from Windows Stat, corrected to check the actual failed operation. No unresolved failures.
- **Next step:** Phase 8, task 1: [phase-8-script-lifecycle.md](phase-8-script-lifecycle.md)
- **Resume cursor if interrupted:** Phase 7 complete; refine Phase 8 script lifecycle against the new replayable body boundary.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

- `execution.Overrides.Body` becomes `*model.Body` (nil keeps saved body); remove the separate BodyMode/byte representation. CLI body flags share one parser: `--body TEXT`, `--body-file PATH`, `--json JSON|@PATH`, repeated `--urlencoded KEY=VALUE`, and repeated `--form KEY=VALUE` / `--form-file KEY=PATH`. Form text is always literal text, including leading @. Multipart and URL-encoded modes cannot mix; multipart text/file flags can mix. Creating a request stores references without opening attachments.
- `model.Body.Text` and `MultipartField.Value` become optional string pointers, retaining explicit empty strings on disk and detecting empty-inline-plus-file conflicts. Legacy raw/JSON objects without text/file continue to mean empty inline content. Validate tag/payload compatibility. File references may carry `file_untrusted` for the future importer; untrusted absolute/outside-workspace and symlink-escaping paths fail before file reads, and editing can remap them inside the workspace.
- `execution.Policy.BodyBase` carries the workspace root for saved requests. Prepare clones/resolves only enabled body entries; raw files and multipart attachments are byte-preserving, JSON files are buffered for one-pass interpolation and validation. No handles survive Prepare. No-script raw files and multipart data stream.
- `internal/httpclient/body.go`: `BuildBody(definition, contentType) (*Body, error)` creates byte/file segments using multipart.Writer framing and ordered URL encoding; `Body.Open(ctx) (io.ReadCloser, error)` opens regular files and streams via io.MultiReader, with idempotent close. No producer goroutine or pipe is needed. Content length and a reopen function enable 307/308 replay through the existing Send entry point. Open failures clean up earlier files and fail before network access. HTTP owns transport close; execution also defers close on all outcomes.
- Explicit raw/JSON/form Content-Type headers win. Multipart requires multipart/form-data: honor an explicit valid boundary, otherwise add the generated boundary to that header; reject conflicting/duplicate multipart Content-Type headers. Optional part filename/content type are supported through native JSON editing; header controls are rejected.
- Standalone relative files use cwd; saved references use workspace root. Imported external attachment paths need explicit remapping.
- Tests cover exact binary bytes, JSON substitution/validation, repeated/disabled form entries, workspace versus cwd paths, saved reference persistence, empty versus absent body, conflicts across all CLI entry points, multipart boundary/metadata, unsafe imported paths, redirect replay and cancellation/early-return cleanup.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestMultipartBytes` — server receives exact file bytes.
- `TestBodyPathBase` — saved relative files resolve at workspace root.
- `TestBodyModeConflict` — incompatible flags fail before send.

## Working examples

```powershell
req send POST https://example.com/upload --body-file ./payload.bin
req send POST https://example.com/items --json '@payload.json' --var count=2
req send POST https://example.com/form --urlencoded 'tag=one' --urlencoded 'tag=two'
req send POST https://example.com/upload --form 'label=example' --form-file 'file=./payload.bin'
req request create API/Upload --method POST --url https://example.com/upload --form-file 'file=./payload.bin'
req run API/Upload
```

These illustrate syntax; automated verification uses loopback servers. Saved file paths resolve from the workspace root, including run overrides. Direct-send file paths resolve from cwd. Raw/attachment bytes are not interpolated; JSON files are buffered for substitution and validation. Imported-path guards are available for Phase 13, but Postman import itself is not implemented yet.
