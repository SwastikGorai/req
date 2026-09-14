# Phase 9 — Variables, request mutation and response APIs

**Goal of this phase (plain English):** Support timestamps, auth headers and response token extraction.

**Dependencies:** Phase 8 checkpoint must be `[x] Done`.

## Do this now

1. [x] Bind scoped variable get/set/has/unset and replaceIn.
2. [x] Bind pre-request header mutation and raw body access.
3. [x] Expose response code/status/headers/text/json/time only after response.
4. [x] Add bounded console logging and clear unknown-API errors.
5. [x] Test token extraction and mutation without changing the saved definition.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 9

- **Status:** `[x] Done`
- **What was actually done:** New internal/scripting/bindings.go (ExecRequest/ResponseData shared types, SetPreRequest/SetPostRequest, goja-Proxy guard with sorted allowlists, full pm.variables/environment/collectionVariables/request/response surface, console caps) with installBindings moved there from loop.go; NewEngine(scope) binds the engine to the variables.Scope (nil → empty). variables.Scope gained ReplaceIn plus a shared lookup helper. execution.Prepare split into MergeOverrides (structural overrides onto a mutable execution copy with a deep-copied Body) + ResolveRequest (in-place variable resolution); Prepare remains as the composed wrapper so existing callers are unchanged; resolveBody resolves in place on the copy; RunLifecycle merges before pre scripts, resolves after them, and hands post scripts a read-only resolved view plus ResponseData. Tests: scripting/bindings_test.go (9 tests), TestReplaceIn, TestPreHeaderMutation, TestPostTokenExtraction, TestResponseUnavailableBeforeSend, CLI TestRunScriptBindings (saved collection byte-identical after run).
- **Verification:** `go test ./internal/scripting` plus the named tests pass; full suite green (see Evidence). Deviations from the LLD: none in behavior; refinements recorded in docs/decisions.md (Phase 9 section).
- **Evidence:** Windows, loopback servers only. `gofmt -l .` empty; `go vet ./...` exit 0; `go build ./...` exit 0; `go test -count=1 ./...` exit 0 (all 7 packages ok, 140 test functions); `go test -race -count=1 ./...` exit 0; `git diff --check` clean. Named tests pass individually (`go test -run 'TestPreHeaderMutation|TestPostTokenExtraction|TestResponseUnavailableBeforeSend' -v ./internal/execution/`): generated header + `{{token}}`/`{{v}}` resolution reached the server, environment layer received the extracted token, `pm.response` in a pre script exits 5 with "not available in pre-request scripts" and zero server hits. Package counts: cli 60, store 28, execution 19, scripting 18, httpclient 8, variables 3, model 4.
- **Next step:** Phase 10, task 1: [phase-10-assertions.md](phase-10-assertions.md)
- **Resume cursor if interrupted:** Complete; nothing open.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

- internal/scripting/bindings.go exposes only the supported pm surface in specification section 8.
- Header upsert replaces all case-insensitive matches. Post-phase request adapter is read-only.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestPreHeaderMutation` — generated header reaches server.
- `TestPostTokenExtraction` — environment overlay receives token.
- `TestResponseUnavailableBeforeSend` — clear script error.
