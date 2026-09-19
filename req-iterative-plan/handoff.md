# Current handoff — Phase 19 final delivery

- **Status:** Phases 0–19 are complete and verified. The Phase 19 acceptance
  and documentation changes are uncommitted; no remote action was performed.
- **Repository:** `D:\Projects\Work\M\GoPM`, branch `main`, baseline commit
  `87866cd` (`Phase 18: structured output and downloads`). Parent integration
  may commit Phase 19; this agent did not commit.
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
- **Changed files:** Added `internal/cli/acceptance_test.go`, the polished
  root `README.md`, focused `docs/{getting-started,cli-reference,scripting,
  import-export}.md`, Phase 19 additions to
  `docs/{compatibility,decisions}.md`, and final updates to
  `req-iterative-plan/{PLAN,handoff.md}` and
  `req-iterative-plan/phases/{index,phase-19-delivery,phase-3-storage}.md`.
  Production code was unchanged.
- **Acceptance:** `TestAcceptanceNativeLogin` persists a login token and uses
  it from a separate profile invocation; `TestAcceptanceImportedScripts`
  imports the existing Postman fixture and verifies callback/Promise and
  inherited script behavior; `TestAcceptanceCurl` verifies CLI cURL
  create/export/import/run wire semantics against a loopback server.
- **Verification:** Focused `rtk go test -count=1 ./internal/cli -run
  'TestAcceptance(NativeLogin|ImportedScripts|Curl)$'` passed 3 tests. Full
  `rtk go test -count=1 -timeout=180s ./...` passed 297 tests across 11
  packages. Race `rtk go test -race -count=1 -timeout=240s ./...` passed the
  same 297 tests. `rtk go vet ./...`, `rtk gofmt -l .`, `rtk go build
  ./cmd/req` and `rtk git diff --check` all passed. Cross-builds passed for
  `windows/amd64`, `linux/amd64`, `darwin/amd64` and `darwin/arm64`; those
  are compile-only checks, not target runtime tests, and artifacts were
  removed from a generated temp directory outside the repository.
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
  script-log redaction. This remains a supported Postman/cURL subset: no GUI,
  cloud sync, collection runner, OAuth UI, cookie jar or remote deployment;
  unsupported Node/Postman APIs and cURL shell/flag forms are warned or
  rejected. Goja is not a hard heap/security sandbox. The README and decisions
  link the primary Goja repository and API docs without claiming otherwise.
- **Next action:** Parent review and commit are the only remaining local
  integration step. Do not publish or deploy remotely without authorization.
