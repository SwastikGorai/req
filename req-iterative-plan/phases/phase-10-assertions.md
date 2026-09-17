# Phase 10 — Tests and supported assertions

**Goal of this phase (plain English):** Report named passing/failing response tests.

**Dependencies:** Phase 9 checkpoint must be `[x] Done`.

## Do this now

1. [x] Implement synchronous pm.test with failure capture and continuation.
2. [x] Implement equality/deep equality/type/property assertion group.
3. [x] Implement inclusion/length/boolean/null/undefined/negation group.
4. [x] Add pm.response.to.have.status and test exit code 6.
5. [x] Test multiple failures and reject async pm.test callbacks explicitly.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 10

- **Status:** `[x] Done`
- **What was actually done:** New internal/scripting/assertions.go: `expectPrelude` (JS shim behind `pm.expect` — proxy-backed chain, strict `===` default, `deep`/`not` flags, to/be/have/and words, equal, a/an, property, include, lengthOf, true/false/null/undefined getters; terminals return a fresh chain so `.and` continues; unknown assertion properties raise instead of silently passing) plus `installAssertions` wiring `pm.test` (records named outcomes into `e.tests`, continue-on-failure, skip-sentinel passthrough, explicit rejection when the callback returns a Promise — checked via `res.Export().(*goja.Promise)`) and `pm.expect` (bridge to the prelude). engine.go: `TestResult` + `Report.Tests`; loop.go: per-run reset, tests in skip/success reports. bindings.go: pm allowlist gains expect/test; `pm.response.to.have.status(code)` via two guarded proxies; pm.response allowlist gains `to`. execution: `codeAssertions = 6`; runPhase prints `ID: PASS/FAIL name[: msg]` and returns assertFailed; RunLifecycle applies `preferAssertions` (6 > 3 > 4 > 0, never 2/5/130) at five return sites, pre+post failures OR together. cli: `exitAssertions = 6`. Deviation from the phase LLD sketch: failing-test messages come from the thrown Error's `message` property (goja's `Exception.Error()` appends the short stack on the same line, so a newline cut alone would leak `at file:line:col`).
- **Verification:** `go test -count=1 ./...` exit 0 (147 test functions, 7 new); `go test -race -count=1 ./...` exit 0; `go vet ./...`, `go build ./...`, `gofmt -l .` clean; `git diff --check` clean. Named tests: TestAssertionsContinue, TestAssertionDeepEqual, TestAsyncTestRejected (all three phase-file stubs), plus TestAssertionChains, TestAssertionFailures, TestResponseStatusAssertion in internal/scripting/assertions_test.go; CLI integration TestRunAssertions in internal/cli/run_test.go (PASS lines + exit 0; FAIL+continue → 6; pre-phase failure still sends → 6; post runtime error → 5 beats 6; failing assertion + --fail 404 → 6 beats 4). Loopback servers only; Windows.
- **Evidence:** Commands run 2026-09-17, all exit 0: `gofmt -l .` (empty), `go vet ./...`, `go build ./...`, `go test -count=1 ./...` (7 packages ok), `go test -race -count=1 ./...` (7 packages ok). Observed assertion behavior pinned by tests: `expected 1 to equal 2`, `expected [1,2] to equal [2,1]` (deep order matters), `expected {"a":1}` extra-keys failure, NaN deep-equals-NaN passes / strict fails, `expected response to have status code 404 but got 200`, `unsupported assertion .exist (supported: …)`, `unsupported pm API "pm.response.to.be" (… supported: have)`, async callbacks rejected with `asynchronous test callbacks are not supported; the callback returned a Promise`.
- **Next step:** Phase 11, task 1: [phase-11-async-callbacks.md](phase-11-async-callbacks.md)
- **Resume cursor if interrupted:** Phase 10 complete; continue at Phase 11 task 1.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

- internal/scripting/assertions.go may use a small audited JS shim with explicit supported chains. *(Used: the shim is the chain itself — JS gives native `===`/`typeof` semantics and a Proxy trap for unknown assertion words.)*
- Do not claim full Chai behavior; each supported chain needs positive and negative fixture cases.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestAssertionsContinue` — failing test does not hide later tests.
- `TestAssertionDeepEqual` — nested values compare correctly.
- `TestAsyncTestRejected` — no premature passing test.
