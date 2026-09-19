# Phase 16 — cURL export

**Goal of this phase (plain English):** Export saved requests with safe quoting and honest omissions.

**Dependencies:** Phase 15 checkpoint must be `[x] Done`.

## Do this now

1. [x] Implement POSIX quoting for headers, URL and body values.
2. [x] Preserve variable placeholders and file references by default.
3. [x] Add explicit --resolve --env for substituted exports.
4. [x] Warn on scripts/nonrepresentable behavior and fail in strict mode.
5. [x] Round-trip supported native requests through export/import and local echo.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 16

- **Status:** `[x] Done`
- **What was actually done:** Added `internal/exporter/curl.go` and `req export curl PATH`. Exports use POSIX single-quote escaping, preserve placeholders and literal file references without reading or rebasing them, resolve only with `--resolve --env NAME`, and represent saved redirect/TLS/auth/body settings. Native relative files use the workspace root during `req run`; exported cURL must run from that corresponding base. Enabled inherited/request scripts, blocked import metadata and unsupported output forms warn; strict export returns before stdout. Added importer support for cURL `--form-string` so literal multipart fields round-trip.
- **Changed files:** `internal/exporter/curl.go`, `internal/exporter/curl_test.go`, `internal/cli/export.go`, `internal/cli/export_test.go`, `internal/cli/root.go`, `internal/importer/curl.go`, `docs/compatibility.md`, `docs/decisions.md`, `req-iterative-plan/PLAN.md`, `req-iterative-plan/phases/index.md`, `req-iterative-plan/phases/phase-16-curl-export.md`, and `req-iterative-plan/handoff.md`.
- **Verification:** `rtk go test -count=1 ./internal/exporter ./internal/cli -run TestCurl` passed; `rtk go test -count=1 -timeout=180s ./...` passed all packages; `rtk go test -race -count=1 -timeout=240s ./...` passed all packages; `rtk go vet ./...`, `rtk gofmt -l internal cmd`, and `rtk git diff --check` passed.
- **Evidence:** `TestCurlRoundTrip` verifies exporter/importer method/query/header/JSON-body recovery and apostrophe quoting; `TestCurlBodyRoundTrip` table-covers raw/JSON inline and file references, urlencoded wire form, multipart literal text and multipart file metadata; `TestCurlScriptsWarn` verifies lenient warning and strict no-command behavior; `TestCurlResolveOptIn` verifies placeholders stay by default and resolved values are quoted; `TestCurlExportCLI` verifies saved-request export, environment resolution and missing `--env` validation; `TestCurlExportLocalEcho` imports the emitted command and runs it against a loopback echo server; `TestCurlExportInheritedScriptsWarnAndStrict` verifies inherited script warnings, strict no-output and no script execution.
- **Next step:** Phase 17, task 1: [phase-17-variable-persistence.md](phase-17-variable-persistence.md)
- **Resume cursor if interrupted:** Phase 17 task 1; review this checkpoint and handoff before editing.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

- internal/exporter/curl.go consumes definitions, not the execution lifecycle.
- Resolved exports can expose secrets; make opt-in behavior clear in help.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestCurlRoundTrip` — supported method/body/query semantics match.
- `TestCurlScriptsWarn` — scripts never execute during export.
- `TestCurlResolveOptIn` — default output retains placeholders.
