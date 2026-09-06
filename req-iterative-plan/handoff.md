# Current handoff

- Status: Phases 0–3 complete and verified; Phase 4 not started.
- Repository/branch/commit or working-tree state: `D:\Projects\Work\M\GoPM`, git `main`: `67f0e05` (Phase 0), `b02a335` (Phase 1), `8f55824` (handoff fix), Phase 2 at HEAD before this handoff (hash self-reference avoided), Phase 3 at HEAD after commit (see `git log`). Working tree clean at handoff. Toolchain Go 1.25.5 (Windows amd64).
- Active phase and exact task: none in progress; next is Phase 4, task 1.
- Changed files and public interfaces (Phase 3):
  - `internal/model/types.go` — `Collection{SchemaVersion, ID, Name, Variables, Auth *Auth, Scripts *Scripts, Items}`, `Item{Type, ID, Name, Folder *Folder, Request *Request}` (exclusive payloads), `Folder{Children, Auth, Scripts}`, `Request{Method, URL, Query, Headers, Auth, Body, Scripts}`, `Entry{Key, Value, Enabled}`, `Auth{Type, Token, Username, Password}`, `Body{Type, Text, File, URLEncoded, Multipart}`, `Scripts`/`Script`/`Provenance`, `SchemaVersion = 1`.
  - `internal/model/validate.go` — `(*Collection).Validate`, `ValidName` (rejects `/`, `\`, `.`/`..`, empty), `ValidID` (`[A-Za-z0-9_-]{1,64}`), `NewID(kind)`, `SchemaVersionError`.
  - `internal/store/workspace.go` — `Init(dir) (*Workspace, created bool, error)` idempotent; `Discover(start)` closest ancestor with `.req`; `Open(path)` explicit root or `.req` dir; `ErrNoWorkspace`.
  - `internal/store/collection.go` — `type Revision string` (SHA-256 of file bytes); `(ws) LoadCollection(ctx, id) (Collection, Revision, error)` strict decode (`DisallowUnknownFields`, trailing data rejected); `(ws) SaveCollection(ctx, c, expected Revision) error` — validate → flock `.req/.lock` (TryLockContext, ctx-aware) → reread/compare → temp+fsync+rename atomic replace; `ConflictError{CollectionID, Have, Want, Recovery}` with candidate preserved under `.req/recovery/`.
  - `internal/cli/init.go` — `req init [--workspace PATH]` (flag accepted before or after subcommand), `parseGlobals` for leading global flags.
  - `internal/cli/root.go` — `exitStorage = 7`; dispatch routes `init`.
  - `go.mod` — added `github.com/gofrs/flock v0.13.1`.
- Verification commands, exit status and observations: `go test -count=1 ./internal/store` → ok; `go vet ./...` clean; `go test -race -count=1 ./...` → ok (cli, httpclient, model, scripting, store); `go build ./cmd/req` OK. Manual smoke: `req init --workspace X` → 0 + correct layout; second init idempotent, user-edited config.json preserved. Full evidence in the phase-3 checkpoint.
- Unfinished criteria / blockers / recovery notes: none. Deferred consciously: empty-vs-absent raw/JSON body representation (Phase 7, see decisions); config.json settings surface (later phases); env file load/save (Phase 6).
- Decisions or deviations and reasons: see docs/decisions.md Phase 3 section (schema shape, `\` in names, revision semantics + recovery file, flock choice, init flag positions).
- Next file/test/action: open [phases/phase-4-saved-requests.md](phases/phase-4-saved-requests.md); refine its LLD against `internal/store`/`internal/model` (collection/folder/request CRUD commands), then implement.

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
