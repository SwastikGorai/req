# Phase 7 — File uploads and body formats

**Goal of this phase (plain English):** Send JSON files, URL-encoded forms and multipart uploads.

**Dependencies:** Phase 6 checkpoint must be `[x] Done`.

## Do this now

1. [ ] Implement --body-file and --json @file with explicit path resolution.
2. [ ] Add URL-encoded repeated fields and disabled entry filtering.
3. [ ] Add multipart text/file fields with streaming and cleanup.
4. [ ] Validate conflicting modes and preserve empty versus absent bodies.
5. [ ] Test file bytes, repeated fields and cancellation cleanup.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 7

- **Status:** `[ ] Not started`
- **What was actually done:** Not implemented. Fill in changed files, decisions and deviations when work occurs.
- **Verification:** `go test ./internal/httpclient` plus the named test cases below; add affected CLI integration tests. Record the actual test names if refined. 
- **Evidence:** Not run. Record command, exit status and observed assertions here.
- **Next step:** Phase 8, task 1: [phase-8-script-lifecycle.md](phase-8-script-lifecycle.md)
- **Resume cursor if interrupted:** Task 1; replace with exact task/test/file before handing off.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

- internal/httpclient/body.go owns body reader construction and close cleanup.
- Standalone relative files use cwd; saved references use workspace root. Imported external attachment paths need explicit remapping.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestMultipartBytes` — server receives exact file bytes.
- `TestBodyPathBase` — saved relative files resolve at workspace root.
- `TestBodyModeConflict` — incompatible flags fail before send.
