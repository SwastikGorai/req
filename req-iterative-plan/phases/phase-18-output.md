# Phase 18 — Structured output and downloads

**Goal of this phase (plain English):** Make responses usable in pipelines and handle large bodies explicitly.

**Dependencies:** Phase 17 checkpoint must be `[x] Done`.

## Do this now

1. [ ] Add versioned JSON envelope with body encoding, logs/tests/errors and skip state.
2. [ ] Add output-file streaming and terminal-only JSON pretty printing.
3. [ ] Implement 10-MiB script/envelope body limit with clear partial-body failure.
4. [ ] Implement documented multi-error exit precedence and redacted metadata.
5. [ ] Test binary body, script logs with JSON stdout, large download and error combinations.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 18

- **Status:** `[ ] Not started`
- **What was actually done:** Not implemented. Fill in changed files, decisions and deviations when work occurs.
- **Verification:** `go test ./internal/output` plus the named test cases below; add affected CLI integration tests. Record the actual test names if refined. 
- **Evidence:** Not run. Record command, exit status and observed assertions here.
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
