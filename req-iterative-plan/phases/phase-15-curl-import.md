# Phase 15 — cURL command import

**Goal of this phase (plain English):** Convert a documented subset without invoking a shell.

**Dependencies:** Phase 14 checkpoint must be `[x] Done`.

## Do this now

1. [x] Implement POSIX tokenization for one curl command and line continuations.
2. [x] Map method/header/URL/data/JSON flags and repeated data semantics.
3. [x] Map auth/form/-G/redirect/TLS flags and file-reference metadata.
4. [x] Reject active shell constructs, unsupported combinations and multiple URLs.
5. [x] Test literal special characters, method inference and redirect defaults.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 15

- **Status:** `[x] Done`
- **What was actually done:** Added a shell-free POSIX-like tokenizer and cURL normalizer for one command. Supported short/long request, header, data, JSON, URL, user, form, GET, location and insecure flags map to the native request model with cURL method inference, repeated-data joining, raw GET query appends, form metadata, basic auth and explicit redirect/TLS policy. Imported file markers are retained as untrusted references and are never read during import. The CLI saves one parsed request through `Workspace.CreateRequest` at `--save-as`; strict warnings fail before persistence.
- **Changed files:** `internal/importer/curl.go`, `internal/importer/curl_test.go`, `internal/cli/import.go`, `internal/cli/curl_import_test.go`, `internal/cli/root.go`, `internal/cli/run.go`, `internal/model/types.go`, `docs/compatibility.md`, `docs/decisions.md`, `req-iterative-plan/PLAN.md`, `req-iterative-plan/phases/index.md`, `req-iterative-plan/phases/phase-15-curl-import.md`, and `req-iterative-plan/handoff.md`.
- **Verification:** All Go tests used a temporary writable `GOCACHE` (removed after verification). Focused cURL tests passed: `rtk go test -count=1 -timeout=120s ./internal/importer -run 'TestCurl'` (9), `rtk go test -count=1 -timeout=120s ./internal/cli -run 'TestCurlImport'` (3), and `rtk go test -count=1 -timeout=120s ./internal/model ./internal/execution` (25). The importer/CLI package subset passed 94 tests. Full and race suites each passed 251 tests in 9 packages with `-timeout=180s` and `-timeout=240s`, respectively. `rtk go vet ./...`, `rtk gofmt -l internal cmd`, and `rtk git diff --check` passed cleanly.
- **Evidence:** `TestCurlQuotedLiteral`, `TestCurlContinuationAndJson`, `TestCurlRejectSubstitution`, `TestCurlNoLocation`, `TestCurlRequestMapping`, `TestCurlGetDataAndFormFiles`, `TestCurlStrictDataFileWarningNoWrite`, `TestCurlRejectUnsafeCombinations`, `TestCurlImportStoresRequest`, `TestCurlImportCLIAndRedirectPolicy`, `TestCurlImportStrictNoWriteCLI` and `TestCurlImportCLIValidation` cover parsing, safety, mapping, provenance, no-read file markers, strict no-write and saved redirect behavior.
- **Next step:** Phase 16, task 1: [phase-16-curl-export.md](phase-16-curl-export.md)
- **Resume cursor if interrupted:** Phase 16 task 1 — inspect the saved request model and implement only cURL export; do not add exporter behavior while reviewing this phase.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

- `internal/importer/curl.go` parses text only and never invokes a shell. Accepted flag behavior follows the [cURL manual](https://curl.se/docs/manpage.html) for method inference, repeated data joining, `--get`, `--json`, file markers and redirect defaults.
- Do not expand shell environment variables or read imported outside-workspace files.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

The deliberate ceiling is `--data @file`: the native body model keeps the
reference and cannot reproduce cURL's file newline/NUL stripping without
reading it during import, so lenient mode warns and strict mode rejects. Form
`<file`, header-file references, stdin data, URL-list files and unsupported
form attributes are rejected rather than silently changed.

## Test stubs

- `TestCurlQuotedLiteral` — single-quoted shell characters remain data.
- `TestCurlRejectSubstitution` — active command substitution is rejected.
- `TestCurlNoLocation` — import without -L does not follow redirects.
