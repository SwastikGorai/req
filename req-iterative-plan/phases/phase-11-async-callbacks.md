# Phase 11 — Auxiliary requests with callbacks

**Goal of this phase (plain English):** Fetch a token in a pre-script before the main request.

**Dependencies:** Phase 10 checkpoint must be `[x] Done`.

## Do this now

1. [x] Extend the production owner loop established in Phase 8 with tracked auxiliary HTTP work.
2. [x] Implement pm.sendRequest string URL and supported request-object conversion.
3. [x] Schedule context-aware HTTP and deliver (err, response) only on owner goroutine.
4. [x] Wait for callback work before advancing scripts and enforce request/concurrency limits.
5. [x] Test nested callbacks, callback errors and authentication before main send.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 11

- **Status:** `[x] Done`
- **What was actually done:** Added callback-only `pm.sendRequest` to the production owner loop. String URLs and request objects with raw/components URLs, ordered/map headers, raw/urlencoded/formdata text bodies, and basic/bearer auth are normalized at scheduling time; saved request headers/auth are not inherited. Auxiliary work is tracked with a FIFO queue (20 requests per execution, 4 active workers), 10 MiB response buffering, context cancellation, generation-checked late completion disposal, and owner-goroutine callback delivery. Callback responses reuse the `pm.response` adapter. Promise return/settlement remains Phase 12.
- **Changed files:** `internal/scripting/async.go`, `internal/scripting/async_test.go`, `internal/scripting/loop.go`, `internal/scripting/bindings.go`, `internal/scripting/engine.go`, `internal/scripting/bindings_test.go`, `internal/execution/lifecycle_test.go`, `docs/decisions.md`.
- **Verification:** `rtk go test -count=1 ./internal/scripting ./internal/execution` and `rtk go test -race -count=1 ./internal/scripting ./internal/execution`; named coverage includes `TestCallbackAuthBeforeMain`, `TestNestedCallbackWait`, `TestCallbackThrows`, `TestRequestObjectConversion`, and `TestAuxiliaryConcurrencyAndLimit`.
- **Evidence:** Focused tests passed (103 tests across scripting and execution). `rtk go test -count=1 -timeout=120s ./...` and `rtk go test -race -count=1 -timeout=180s ./...` both passed (209 tests across 8 packages); `rtk go vet ./...` and `rtk git diff --check` passed.
- **Next step:** Phase 12, task 1: [phase-12-async-promises.md](phase-12-async-promises.md)
- **Resume cursor if interrupted:** Phase 11 complete; begin Phase 12 Promise return/settlement tracking. Do not treat callback-only completion as Promise support.

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
