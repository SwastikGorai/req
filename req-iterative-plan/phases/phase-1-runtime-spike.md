# Phase 1 — Embedded JavaScript feasibility

**Goal of this phase (plain English):** Prove the riskiest integration after the HTTP core works.

**Dependencies:** Phase 0 checkpoint must be `[x] Done`.

## Do this now

1. [ ] Evaluate and pin a Goja version compatible with the installed Go toolchain; record its version and license.
2. [ ] Create internal/scripting/spike_test.go with a JS variable adapter and source-location error test.
3. [ ] Add a deadline test that interrupts an infinite JS loop without hanging the test process.
4. [ ] Build a minimal owner-goroutine queue; complete one local HTTP callback on that owner.
5. [ ] Complete one Promise request and cancel one delayed HTTP request; run race tests and record the runtime decision.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 1

- **Status:** `[ ] Not started`
- **What was actually done:** Not implemented. Fill in changed files, decisions and deviations when work occurs.
- **Verification:** `go test -race -timeout 30s ./internal/scripting -run TestRuntime`; all callback, Promise, interrupt and cancellation cases must finish.
- **Evidence:** Not run. Record command, exit status and observed assertions here.
- **Next step:** Phase 2, task 1: [phase-2-http-options.md](phase-2-http-options.md)
- **Resume cursor if interrupted:** Task 1; replace with exact task/test/file before handing off.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Initial contracts are specified below.*

- internal/scripting/engine.go defines type Source struct { Name string; Code string }; type Report struct { Logs []string }; type Engine interface { Run(context.Context, Source) (Report, error); Close() error }. Extend reports only when production bindings are implemented.
- internal/scripting/loop.go owns runtime execution; HTTP goroutines enqueue result closures and never touch JS values. The documented interrupt API may be called by the deadline watchdog.
- Preferred candidate is github.com/dop251/goja, subject to these tests; record the actual pinned version in docs/decisions.md. A JS engine does not provide Postman APIs automatically.
- Count pending host tasks before launching work; decrement after its owner-thread callback settles. A zero HTTP count alone is insufficient if Promise reactions still need draining.
- Use httptest for delayed completion. Put an outer test timeout around infinite-loop cases so a broken watchdog fails mechanically.
- Interrupting JS does not interrupt a blocking native Go function; every HTTP host binding also receives the context. No production pm compatibility is claimed by this spike.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestRuntimeVariableRoundTrip` — JS set/get reaches scoped Go data.
- `TestRuntimeInterrupt` — infinite JS completes with timeout.
- `TestRuntimeCallbackAndPromise` — both wait for local HTTP completion.
- `TestRuntimeCancellation` — context cancellation releases pending HTTP work.
