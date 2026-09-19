# Current handoff — Phase 18

- **Status:** Phases 0–18 are complete and verified. Phase 19 (integrated
  acceptance and final delivery) remains.
- **Repository:** `D:\Projects\Work\M\GoPM`, branch `main`, baseline commit
  `4fe2efc` (`Phase 17: persist extracted variables safely`). Phase 18 changes
  are intentionally uncommitted; no remote action was performed. This corrects
  the stale handoff that pointed at the Phase 15/16 baseline.
- **Implementation:** `req send` and `req run` share `--output PATH`, `--raw`,
  `--verbose` and `--output-format json`. Default and raw paths preserve
  streaming body output; terminal-only valid JSON is pretty-printed when raw is
  absent. JSON emits one version-1 envelope with status, ordered headers,
  duration, body text/base64, tests, logs, errors and skipped state. Output
  files use same-directory temporary files and replacement, and JSON references
  the literal output path instead of duplicating bytes.
- **Lifecycle:** Script and JSON body reads stop at 10 MiB without running post
  scripts on partial data. A no-script output-file download remains unbounded.
  Script reports and persistence errors aggregate into the envelope, while exit
  precedence remains 130 > 7 > 5 > 6 > 3 > 4. Header metadata redacts
  Authorization, Proxy-Authorization, Cookie, Set-Cookie and configured
  `config.json` `secret_headers`; bodies and script logs are not redacted.
- **Changed files:** `internal/output/render.go` and tests,
  `internal/store/config.go`, `internal/httpclient/client.go`,
  `internal/scripting/engine.go`, `internal/execution/{execution,prepare,run,lifecycle}.go`
  and tests, `internal/cli/{output,send,run,root}.go` and tests,
  `docs/{compatibility,decisions}.md`, and the Phase 18 trackers.
- **Verification:** Focused `rtk go test -count=1 ./internal/output ./internal/execution ./internal/cli ./internal/store ./internal/httpclient ./internal/scripting` passed (253 tests across 6 packages). Full `rtk go test -count=1 -timeout=180s ./...` passed (294 tests across 11 packages). Race `rtk go test -race -count=1 -timeout=240s ./...` passed (294 tests across 11 packages). `rtk go vet ./...`, `rtk gofmt -l internal cmd`, and `rtk git diff --check` all passed.
- **Named output tests:** `TestJSONSingleEnvelope`,
  `TestLargeBodyScriptFailure`, `TestRawDownloadStreaming`,
  `TestBinaryBase64Envelope`, `TestTerminalOnlyPrettyJSON`,
  `TestOutputPathReference`, `TestOutputPathFailureLeavesDestination`,
  `TestHeaderRedaction`, `TestDirectJSONSendBlocksOnRecoveryError`, and the
  JSON script diagnostics assertion in `TestRunAssertions` pass.
- **Assumptions and limits:** Output paths are interpreted by the CLI working
  directory. Native saved relative attachment files still execute relative to
  the workspace root; cURL export retains literal spelling and must run from
  the corresponding base, and imported source references may need remapping as
  already documented. JSON/output files intentionally do not promise body or
  script-log redaction.
- **Next action:** Begin [phase-19-delivery.md](phases/phase-19-delivery.md),
  task 1.
