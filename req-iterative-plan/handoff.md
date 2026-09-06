# Current handoff

- Status: Phases 0–4 complete and verified; Phase 5 not started.
- Repository/branch/commit or working-tree state: `D:\Projects\Work\M\GoPM`, git `main`, working tree clean after the Phase 4 commit at HEAD (see `git log`). Toolchain Go 1.25.5 (Windows amd64).
- Active phase and exact task: none in progress; next is Phase 5, task 1.
- Changed files and public interfaces (Phase 4):
  - `internal/store/selectors.go` — `SplitPath(path) ([]string, error)` (per-segment `model.ValidName`), `ErrInvalidPath` wrapping; `ResolvedPath{Collection, Rev, Segments, Item}`; `(ws) ResolvePath(ctx, path)` (collection by unique Name; missing → `ErrNotFound` naming the segment; through-a-request → distinct error).
  - `internal/store/collections.go` — `(ws) ListCollections(ctx) ([]model.Collection, error)` (strict loads, sorted by name); `(ws) CreateCollection(ctx, name) (model.Collection, error)` (duplicate → `ErrDuplicateName`).
  - `internal/store/mutations.go` — `(ws) CreateFolder(ctx, path, parents) ([]string, error)` (created names, innermost last); `(ws) CreateRequest(ctx, path, req model.Request, parents) error`; shared walk `createItem`; persistence under load-time `Revision`.
  - `internal/store/errors.go` — added sentinels `ErrInvalidPath`, `ErrDuplicateName`.
  - `internal/execution` (new package) — `Outgoing`, `Overrides{Method,URL; Queries,Headers [][2]string; Body []byte; BodyMode string}`, `Policy{Timeout, InsecureTLS, FollowRedirects, FailOnHTTPError}`; `Prepare(saved model.Request, ov Overrides, pol Policy) (Outgoing, error)` (enabled entries only; append-vs-replace override semantics; Content-Type defaults; remaining `{{…}}`/URL/method/Phase-7-body errors); `Execute(ctx, Outgoing, stdout, stderr) int` (status line → stderr, body → stdout, exit 0/3/4/130).
  - `internal/cli/send.go` — delegates to `execution.Prepare`/`Execute`; headers now ordered `[][2]string`; shared `parseHeaderEntry`/`parseQueryEntry`.
  - `internal/cli` new handlers — `collection.go` (create/list), `folder.go` (create [--parents]), `request.go` (create/list/show), `tree.go`, `run.go` (override + policy flags; never persists), `workspace.go` (`openWorkspace`, `usageOrStorage`: sentinels → 2, else 7). `root.go` routes `collection|folder|request|tree|run` and lists them in usage.
- Verification commands, exit status and observations: `go vet ./...` clean; `go test -count=1 ./...` ok; `go test -race -count=1 ./...` ok (cli/httpclient/model/scripting/store); `go build ./...` ok. Named tests in the phase-4 checkpoint (store: 8 new incl. `TestCreateFolderParents`/`TestCreateFolderDuplicateName`; cli: 12 new incl. `TestSavedRunAfterReload`, `TestRunOverrides`, `TestRunUnresolvedPlaceholder`); Phase 2 send tests pass unchanged. Manual binary smoke of the full CRUD → tree/show/list → run-placeholder(2) → not-found(2) → leading `--workspace` flow: all as specified. Full evidence in the phase-4 checkpoint.
- Unfinished criteria / blockers / recovery notes: none. Deferred consciously: rename/move/delete/editor (Phase 5), env/vars/auth flags on run (Phase 6), urlencoded/multipart/file bodies (Phase 7). Known pre-existing, out of Phase 4 scope: `gofmt -l internal/` flags three Phase 0–3 files (`httpclient/build_test.go`, `model/types.go`, `model/validate_test.go`) for line endings — normalize in a dedicated cleanup if desired.
- Decisions or deviations and reasons: see docs/decisions.md Phase 4 section (name-addressed paths, `--parents` scope, shared execution path, placeholder rejection incl. direct send, exit-code split, output channels, tree format).
- Next file/test/action: open [phases/phase-5-editing.md](phases/phase-5-editing.md); refine its LLD against `internal/store/mutations.go` (extend for rename/move/delete), the revision/recovery flow, and editor launching; then dispatch to `subagent-implementer` per the working mode below.

## Working mode from Phase 4 onward (user-authorized)

The user defined a `subagent-implementer` agent at `C:/Users/swast/.zcode/agents/subagent-implementer.md` (user-level, model GLM-5.3-Flash, tools Read/Grep/Glob/Bash/Edit/Write) and authorized the orchestrator/worker split. From Phase 4 onward the main agent acts as orchestrator only: research/refine LLDs, define tasks with acceptance criteria, grant command approvals (go vet/test/build commands used by earlier phases are pre-approved patterns to re-confirm per task), dispatch all code changes to `subagent-implementer`, verify its evidence independently, then update trackers and commit itself (the subagent is forbidden to commit). If the agent type is not available in the session's dispatch list, surface that to the user instead of silently reverting to direct implementation.

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
