# Current handoff

- Status: Phases 0–2 complete and verified; Phase 3 not started.
- Repository/branch/commit or working-tree state: `D:\Projects\Work\M\GoPM`, git `main`: `67f0e05` (Phase 0), `b02a335` (Phase 1), `8f55824` (handoff fix), Phase 2 at HEAD (squashed tracker updates; hash not embeddable without self-reference). Working tree clean at handoff. Toolchain Go 1.25.5 (Windows amd64).
- Active phase and exact task: none in progress; next is Phase 3, task 1.
- Changed files and public interfaces (Phase 2):
  - `internal/httpclient/build.go` — `type Options { Timeout time.Duration; InsecureTLS, FollowRedirects bool }`, `func Client(opts) *http.Client` (0 timeout → 30s; `--no-follow` via `http.ErrUseLastResponse`; strips sensitive headers when a redirect leaves the original origin — scheme+host+port, stricter than net/http). `client.go`: `DefaultClient() = Client(Options{})`; `Send` unchanged.
  - `internal/cli/send.go` — `runSend` + `parseSendArgs`/`applyQueries`: flags `-H/--header` (append), `--query` (append onto existing query without re-encoding), `--method`/`--url`, `--body`/`--json` (exclusive; JSON validated pre-send; CT text/plain / application/json, explicit header wins), `--timeout`, `--no-follow`, `--insecure`, `--fail` (exit 4 on status >= 400). Exit codes 0/2/3/4/130. root.go is dispatch/usage/version only.
  - Tests: `internal/httpclient/build_test.go` (TestClientPolicy, TestSameOrigin, TestClientRedirectPolicy); `internal/cli/send_test.go` (TestHTTPRepeatedQuery, TestHTTPJSON, TestHTTPFailFlag, TestHTTPRedirectSensitiveHeaders, TestHTTPRedirectSameOriginKeepsHeaders, TestSendValidation, TestSendBodyModes, TestSendRedirectNoFollow, TestSendInsecureTLS, TestSendTimeout); root_test.go keeps the three Phase 0 core tests.
- Verification commands, exit status and observations: `go test ./internal/httpclient` → ok; `go vet ./...` clean; `go test -race -count=1 ./...` → ok (cli, httpclient, scripting); `go build ./cmd/req` OK. Manual smoke confirmed query appending (`?a=1&tag=x&tag=y`), `--fail` exit 4 on 404, and exit 2 on conflicting body flags. Full evidence in the phase-2 checkpoint.
- Unfinished criteria / blockers / recovery notes: none. Note: auth flags are Phase 6; `--body-file`/form modes are Phase 7; `--output/--raw/--verbose/--output-format` are Phase 18 — deliberately absent.
- Decisions or deviations and reasons: origin-strict redirect credential stripping (see docs/decisions.md) — net/http forwards credentials across ports and on https→http downgrades, which the spec's "unrelated origins" rule forbids. No other deviations from the phase LLD or IMPLEMENTATION.md.
- Next file/test/action: open [phases/phase-3-storage.md](phases/phase-3-storage.md); refine its LLD (workspace discovery, atomic writes, revision checks, mutation lock) against the current code, then implement `internal/store` and `req init`.

## Replace/update when handing off implementation

- Repository/branch/commit or working-tree state: (filled above)
- Active phase and exact task: (filled above)
- Changed files and public interfaces: (filled above)
- Verification commands, exit status and observations: (filled above)
- Unfinished criteria / blockers / recovery notes: (filled above)
- Decisions or deviations and reasons: (filled above)
- Next file/test/action: (filled above)

## Replace/update when handing off implementation

- Repository/branch/commit or working-tree state: (filled above)
- Active phase and exact task: (filled above)
- Changed files and public interfaces: (filled above)
- Verification commands, exit status and observations: (filled above)
- Unfinished criteria / blockers / recovery notes: (filled above)
- Decisions or deviations and reasons: (filled above)
- Next file/test/action: (filled above)
