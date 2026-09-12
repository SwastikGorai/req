# Phase 11 — Auxiliary requests with callbacks

**Goal of this phase (plain English):** Fetch a token in a pre-script before the main request.

**Dependencies:** Phase 10 checkpoint must be `[x] Done`.

## Do this now

1. [ ] Extend the production owner loop established in Phase 8 with tracked auxiliary HTTP work.
2. [ ] Implement pm.sendRequest string URL and supported request-object conversion.
3. [ ] Schedule context-aware HTTP and deliver (err, response) only on owner goroutine.
4. [ ] Wait for callback work before advancing scripts and enforce request/concurrency limits.
5. [ ] Test nested callbacks, callback errors and authentication before main send.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 11

- **Status:** `[ ] Not started`
- **What was actually done:** Not implemented. Fill in changed files, decisions and deviations when work occurs.
- **Verification:** `go test ./internal/scripting` plus the named test cases below; add affected CLI integration tests. Record the actual test names if refined. 
- **Evidence:** Not run. Record command, exit status and observed assertions here.
- **Next step:** Phase 12, task 1: [phase-12-async-promises.md](phase-12-async-promises.md)
- **Resume cursor if interrupted:** Task 1; replace with exact task/test/file before handing off.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

- internal/scripting/async.go tracks pending work; defaults are 20 requests and 4 concurrent per execution.
- Auxiliary requests use HTTP services but run no inherited scripts and inherit no main-request credentials.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestCallbackAuthBeforeMain` — token exists when main is built.
- `TestNestedCallbackWait` — nested request settles before phase advances.
- `TestCallbackThrows` — error becomes script failure.
