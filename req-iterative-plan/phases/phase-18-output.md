# Phase 18 — Structured output and downloads

**Goal of this phase (plain English):** Make responses usable in pipelines and handle large bodies explicitly.

**Dependencies:** Phase 17 checkpoint must be `[x] Done`.

## Do this now

1. [x] Add versioned JSON envelope with body encoding, logs/tests/errors and skip state.
2. [x] Add output-file streaming and terminal-only JSON pretty printing.
3. [x] Implement 10-MiB script/envelope body limit with clear partial-body failure.
4. [x] Implement documented multi-error exit precedence and redacted metadata.
5. [x] Test binary body, script logs with JSON stdout, large download and error combinations.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 18

- **Status:** `[x] Done`
- **What was actually done:** Added shared send/run output flags and one
  `internal/output` renderer. Default output remains streamed body-only;
  terminal JSON is pretty-printed only for terminals, JSON mode emits one
  version-1 envelope, and `--output` atomically replaces a destination after a
  complete body. Script/JSON buffers stop at 10 MiB, while no-script output
  downloads stream without that cap. Header metadata redacts built-in and
  configured secret names; body and script-log content is intentionally not
  redacted. Lifecycle aggregation keeps tests, logs, errors, skipped state and
  exit precedence together.
- **Verification:** Focused `rtk go test -count=1 ./internal/output ./internal/execution ./internal/cli`
  passed (120 tests), including `TestJSONSingleEnvelope`,
  `TestLargeBodyScriptFailure`, `TestRawDownloadStreaming`,
  `TestBinaryBase64Envelope`, `TestTerminalOnlyPrettyJSON`,
  `TestOutputPathReference`, `TestHeaderRedaction`,
  `TestDirectJSONSendBlocksOnRecoveryError` and combined precedence.
  Full `rtk go test -count=1 -timeout=180s ./...` and race
  `rtk go test -race -count=1 -timeout=240s ./...` each passed (294 tests);
  `rtk go vet ./...`, `rtk gofmt -l internal cmd` and `rtk git diff --check`
  exited 0.
- **Evidence:** The final gate commands passed with no failures; focused tests
  cover envelope single-value output, binary base64, terminal/pipeline bytes,
  atomic output-path references and failure recovery, header redaction, body-limit script failure
  and combined exit precedence. Direct JSON/verbose sends also stop on
  actionable recovery errors rather than swallowing them. Full evidence is
  mirrored in `handoff.md`.
- **Next step:** Phase 19, task 1: [phase-19-delivery.md](phase-19-delivery.md)
- **Resume cursor if interrupted:** Task 1; replace with exact task/test/file before handing off.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

- internal/output/render.go accepts aggregated execution results; keep diagnostics on stderr.
- Response bodies/script logs can contain secrets; header redaction is not a promise to sanitize arbitrary content.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestJSONSingleEnvelope` — logs never corrupt stdout JSON.
- `TestLargeBodyScriptFailure` — scripts never see truncated JSON.
- `TestRawDownloadStreaming` — no-script download streams.
