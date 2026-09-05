# Phase 10 — Tests and supported assertions

**Goal of this phase (plain English):** Report named passing/failing response tests.

**Dependencies:** Phase 9 checkpoint must be `[x] Done`.

## Do this now

1. [ ] Implement synchronous pm.test with failure capture and continuation.
2. [ ] Implement equality/deep equality/type/property assertion group.
3. [ ] Implement inclusion/length/boolean/null/undefined/negation group.
4. [ ] Add pm.response.to.have.status and test exit code 6.
5. [ ] Test multiple failures and reject async pm.test callbacks explicitly.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 10

- **Status:** `[ ] Not started`
- **What was actually done:** Not implemented. Fill in changed files, decisions and deviations when work occurs.
- **Verification:** `go test ./internal/scripting` plus the named test cases below; add affected CLI integration tests. Record the actual test names if refined. 
- **Evidence:** Not run. Record command, exit status and observed assertions here.
- **Next step:** Phase 11, task 1: [phase-11-async-callbacks.md](phase-11-async-callbacks.md)
- **Resume cursor if interrupted:** Task 1; replace with exact task/test/file before handing off.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

- internal/scripting/assertions.go may use a small audited JS shim with explicit supported chains.
- Do not claim full Chai behavior; each supported chain needs positive and negative fixture cases.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestAssertionsContinue` — failing test does not hide later tests.
- `TestAssertionDeepEqual` — nested values compare correctly.
- `TestAsyncTestRejected` — no premature passing test.
