# Decision log

Consequential implementation choices, newest phase first. Routine reversible choices stay in code; cross-phase consequences live here and in handoff.md.

## Phase 2 — HTTP methods, bodies and failures

- **Credentials follow redirects only within the exact origin (scheme + host + port), stricter than net/http.** Verified against the Go 1.25 source: `Client.do` compares domain names (`isDomainOrSubdomain` over IDNA hosts), so the stdlib forwards `Authorization`/`Cookie` across ports and on an https→http downgrade to the same host. Our `Client(Options).CheckRedirect` deletes the sensitive header set (`Authorization`, `Proxy-Authorization`, `Cookie`, `Cookie2`, `Www-Authenticate`, `Proxy-Authenticate`) whenever the redirect leaves the original origin, and keeps them on same-origin redirects. Subdomain forwards are also stripped (no `--location-trusted` equivalent until a phase asks for one); explicit `:80`-style ports are compared literally, so a default-port URL may be over-stripped — the fail-safe direction. This behavior applies to main and (later) auxiliary script requests because both use `httpclient.Client`.
- **Client policy moved into `Options`/`Client(opts)`** (`internal/httpclient/build.go`); `DefaultClient()` = `Client(Options{})`; `Send` untouched. Phase 3+ selects per-execution policy (timeout, insecure, redirects) through `Options`.
- **Send flag parsing is strict and additive.** Repeated `-H/--query` append entries; `--query` appends onto the existing URL query without re-encoding it (duplicates and empty values survive byte-for-byte). `--body`/`--json` are the only body modes until Phase 7; JSON is validated (exit 2) before sending; generated Content-Type (`application/json` / `text/plain`) loses to any explicit `Content-Type` header. Positional `METHOD URL` and `--method`/`--url` are mutually exclusive; unknown flags and extra positional arguments exit 2.
- **Go 1.25 renamed the redirect sentinel:** `http.ErrUseLastResponse` (the old `ErrUseOfLastResponse` no longer exists) — used for `--no-follow`.

## Phase 1 — Embedded JavaScript feasibility

- **Runtime chosen: `github.com/dop251/goja`, pinned at `v0.0.0-20260903201622-f87b40ad7341`.** Resolved via `go get @latest` on 2026-09-05 with Go 1.25.5; commit dated 2026-09-03, so the project is actively maintained. License: MIT (verified in the module cache). All five feasibility tests pass under `-race`.
- **Goja fact that shapes the event loop:** this version has no `ExecuteDeferredJobs`; promise reaction jobs run automatically whenever the JS stack empties — after `RunProgram` and after each owner-thread `Callable` invocation. The drain loop therefore only needs the pending-host-task counter; reactions that schedule more host work raise the counter again before the check.
- **Interrupt handling:** `rt.Interrupt` is the only cross-goroutine runtime call (watchdog only). It latches if the runtime is idle, so each run starts with `ClearInterrupt()` — safe because the previous run's watchdog has provably exited before `Run` returns. `Run` reports either `*goja.InterruptedError` or the context error for the same cancellation; tests accept both, and the production exit-code mapping (130/3) is applied later.
- **Late completions:** every completion job carries the run-generation token of the run that scheduled it; if the run has ended (or its context is gone) the job is discarded without touching JS values. `Close` drops queued jobs via a `quit` channel so senders never block or panic.
- **Spike bindings (`vars`, `log`, `httpGet`, `httpGetAsync`) are throwaway.** They prove ownership/interruption/async only; the supported pm surface is built in Phases 8–12 and is not wired into the CLI yet. Spike body cap is a fixed 1 MiB placeholder; the configurable 10 MiB policy comes with production bindings.

## Phase 0 — Core / Walking Skeleton

- **Module path is `req`, module rooted at the repository root.** No hosting/domain prefix; the binary is the deliverable and nothing is published. Imports read `req/internal/...`. No third-party dependencies yet.
- **Argument parsing is strict positional parsing inside `internal/cli`, not the `flag` package yet.** `req send METHOD URL` must be exactly two arguments; anything else (unknown command, missing/extra args, schemeless or non-http(s) URL) exits 2 with usage. Nothing is silently dropped; flags join in Phase 2 ("keep argument parsing replaceable").
- **A status/duration line goes to stderr on every successful send** (`GET url -> 200 OK in 1.2ms`), per IMPLEMENTATION.md §6 ("stderr contains status, elapsed time"). Stdout stays body-only.
- **Context cancellation maps to exit 130** (`signal.NotifyContext` in main; `runSend` checks `ctx.Err()` after a Send error). Transport errors otherwise map to 3; body-read failure after received headers also maps to 3.
- **`httpclient.Send` rejects non-http(s) schemes defensively** even though the CLI already validates (exit 2 first). The CLI check owns the user-facing message.
- **`Version = "0.0.1"` is a constant in `internal/cli`**; `--help`/`help` prints usage to stdout with exit 0. Build-info stamping deferred; trivial to add at delivery.
- **No VCS initialized.** The directory was not a git repository and none was requested; handoff.md tracks working-tree state instead. Initialize git whenever the user wants per-phase commits.
