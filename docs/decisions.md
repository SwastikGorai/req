# Decision log

Consequential implementation choices, newest phase first. Routine reversible choices stay in code; cross-phase consequences live here and in handoff.md.

## Phase 0 — Core / Walking Skeleton

- **Module path is `req`, module rooted at the repository root.** No hosting/domain prefix; the binary is the deliverable and nothing is published. Imports read `req/internal/...`. No third-party dependencies yet.
- **Argument parsing is strict positional parsing inside `internal/cli`, not the `flag` package yet.** `req send METHOD URL` must be exactly two arguments; anything else (unknown command, missing/extra args, schemeless or non-http(s) URL) exits 2 with usage. Nothing is silently dropped; flags join in Phase 2 ("keep argument parsing replaceable").
- **A status/duration line goes to stderr on every successful send** (`GET url -> 200 OK in 1.2ms`), per IMPLEMENTATION.md §6 ("stderr contains status, elapsed time"). Stdout stays body-only.
- **Context cancellation maps to exit 130** (`signal.NotifyContext` in main; `runSend` checks `ctx.Err()` after a Send error). Transport errors otherwise map to 3; body-read failure after received headers also maps to 3.
- **`httpclient.Send` rejects non-http(s) schemes defensively** even though the CLI already validates (exit 2 first). The CLI check owns the user-facing message.
- **`Version = "0.0.1"` is a constant in `internal/cli`**; `--help`/`help` prints usage to stdout with exit 0. Build-info stamping deferred; trivial to add at delivery.
- **No VCS initialized.** The directory was not a git repository and none was requested; handoff.md tracks working-tree state instead. Initialize git whenever the user wants per-phase commits.
