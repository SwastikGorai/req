# Phase 1 — Embedded JavaScript feasibility

**Goal of this phase (plain English):** Prove the riskiest integration after the HTTP core works.

**Dependencies:** Phase 0 checkpoint must be `[x] Done`.

## Do this now

1. [x] Evaluate and pin a Goja version compatible with the installed Go toolchain; record its version and license.
2. [x] Create internal/scripting/spike_test.go with a JS variable adapter and source-location error test.
3. [x] Add a deadline test that interrupts an infinite JS loop without hanging the test process.
4. [x] Build a minimal owner-goroutine queue; complete one local HTTP callback on that owner.
5. [x] Complete one Promise request and cancel one delayed HTTP request; run race tests and record the runtime decision.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 1

- **Status:** `[x] Complete`
- **What was actually done:** Pinned `github.com/dop251/goja v0.0.0-20260903201622-f87b40ad7341` (MIT license verified in the module cache; commit dated 2026-09-03, actively maintained; resolved via `go get @latest` on Go 1.25.5). Created `internal/scripting/engine.go` (`Source`, `Report`, `Engine` exactly per LLD) and `internal/scripting/loop.go`: one owner goroutine owns the `goja.Runtime` through an unbuffered job queue; a watchdog calls `rt.Interrupt(ctx.Err())` (Goja's documented cross-goroutine exception) and a latched interrupt is cleared at the next run start after the previous watchdog provably exited; each run gets a generation token so late completions are discarded; host HTTP reuses `internal/httpclient` with the per-run context, counts `pending` before launch and decrements it only after the owner-thread callback settles. The drain loop returns when `pending == 0` — in this Goja version promise reaction jobs run automatically whenever the JS stack empties (after `RunProgram` and after each owner-thread `Callable` call; there is no `ExecuteDeferredJobs`), which satisfies the "zero HTTP count alone is insufficient" rule structurally. Spike bindings: `vars` (Go-backed store), `log`, `httpGet` (callback), `httpGetAsync` (Goja `NewPromise`, resolved on the owner). Tests: the four named stubs plus `TestRuntimeSourceLocationError`. Spike is not wired into the CLI and claims no pm compatibility.
- **Verification:** `go test -race -timeout 30s ./internal/scripting -run TestRuntime`; `go vet ./...`; `go test -race ./...`; `go build ./cmd/req`.
- **Evidence:** Official phase command: `ok req/internal/scripting` (1.497s; verbose uncached run 3.197s). All five tests PASS under `-race`: TestRuntimeVariableRoundTrip, TestRuntimeSourceLocationError, TestRuntimeInterrupt (0.10s — watchdog interrupted `for(;;){}` at the 100ms deadline), TestRuntimeCallbackAndPromise (callback and Promise both settled on the owner; logs prove post-script draining: `script start`, `script end`, `promise settled`), TestRuntimeCancellation (canceled mid-flight; Run returned `context.Canceled`, the server handler was released, no success callback). `go vet ./...` clean; full race suite `ok` for `internal/cli`, `internal/httpclient`, `internal/scripting`; build OK. Runtime decision recorded in [docs/decisions.md](../../docs/decisions.md).
- **Next step:** Phase 2, task 1: [phase-2-http-options.md](phase-2-http-options.md)
- **Resume cursor if interrupted:** Phase 1 complete; start Phase 2 by refining its LLD signatures against the actual `internal/httpclient`/`internal/cli` code (AGENTS.md working loop step 3), then implement.

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
