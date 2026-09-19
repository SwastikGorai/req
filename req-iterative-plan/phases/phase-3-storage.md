# Phase 3 — Workspace and atomic persistence

**Goal of this phase (plain English):** Save a validated collection file and reload it safely.

**Dependencies:** Phase 2 checkpoint must be `[x] Done`.

## Do this now

1. [x] Create versioned model structs and one synthetic collection fixture.
2. [x] Implement req init and closest-ancestor workspace discovery.
3. [x] Implement load/validate and atomic same-directory file replacement.
4. [x] Add workspace mutation lock plus expected-content-hash conflict check.
5. [x] Test malformed files, future versions, round trips and conflicting writers.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 3

- **Status:** `[x] Complete`
- **What was actually done:** `internal/model` (new): `types.go` with the section-4 schema (`Collection`, tagged `Item` (`Folder`/`Request` payloads), `Entry` `{key,value,enabled}` ordered lists, tagged `Auth` (`inherit`/`none`/`bearer`/`basic`, nil = inherit), tagged `Body` (all five modes), `Scripts`/`Script` with optional `Provenance`, `NewID`); `validate.go` with `Validate()` (schema version, ID/name rules — both separators rejected since `\` is one on Windows, sibling-name and tree-wide-ID uniqueness, auth/body tag checks, text/file exclusivity, `env:` reserved variable prefix, duplicate script IDs). `internal/store` (new): `Workspace` with `Discover` (closest ancestor containing `.req`), `Open` (explicit path or `.req` itself), `Init` (idempotent: creates `.req/{config.json,collections/,environments/,recovery/,.gitignore}`, never overwrites existing config/gitignore); `LoadCollection` (strict decode: `DisallowUnknownFields`, trailing-data rejection, full validation, SHA-256 content-hash `Revision`); `SaveCollection` (validate → marshal → flock workspace lock → reread/compare revision → temp-file + fsync + atomic rename; conflicts preserve the candidate in `.req/recovery/<id>-<hash>.json` and return `*ConflictError` with the recovery path). Dependency added: `github.com/gofrs/flock v0.13.1` (MIT) for the cross-platform mutation lock. CLI: `req init [--workspace PATH]` (flag accepted before or after the subcommand; I/O failures exit 7, bad usage exit 2); global-flag parsing scaffolding in `parseGlobals`. Fixture: `internal/store/testdata/synthetic-collection.json` (nested folder, duplicate/disabled entries, scripts, auth refs, JSON body).
- **Deviations/refinements:** names reject `\` in addition to `/` (path-separator safety on Windows); raw/JSON bodies currently permit neither text nor file (empty-vs-absent body representation is deferred to Phase 7 with a note in docs/decisions.md); config.json holds `{"schema_version":1}` pending later settings.
- **Verification:** `go test -count=1 ./internal/store`; `go vet ./...`; `go test -race -count=1 ./...`; `go build ./cmd/req`.
- **Evidence:** All exit 0; full race suite `ok` for cli/httpclient/model/scripting/store. Named tests PASS: `TestStoreRoundTrip` (IDs `fld-auth`/`req-login`/`req-ping` survive reload; deep structure equal), `TestStoreConflict` (stale revision rejected with recovery file preserved on disk; winner's write intact; create-over-existing also conflicts), `TestStoreFutureSchema` (schema_version 99 → `model.SchemaVersionError`; file bytes untouched even by save attempts). Additional: `TestStoreFixtureLoads`, `TestStoreMalformed` (garbage/unknown-field/trailing-data/validation cases never rewrite the file), `TestStoreConcurrentWriters` (8 racing writers, exactly 1 wins under `-race`), `TestStoreLockBlocksDuringSave` (a save cannot complete while the lock is held), `TestInitIdempotent`, `TestDiscoverClosestAncestor`, `TestOpenExplicit`; model: 22-case rejection table + name/ID tests; CLI: init creates/idempotent/leading global flag/argument rejection. Manual smoke verified `req --workspace X init` → 0 with correct layout; second init → "already initialized" and user-edited config.json preserved.
- **Next step:** Phase 4, task 1: [phase-4-saved-requests.md](phase-4-saved-requests.md)
- **Resume cursor if interrupted:** Phase 3 complete; start Phase 4 task 1 (collections/folders/saved requests CRUD on top of `internal/store`) against the current working tree.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

- internal/model/types.go owns Collection, Item, Request, Body and Script types from IMPLEMENTATION.md section 4.
- internal/store/store.go: LoadCollection(ctx context.Context, id string) (Collection, Revision, error); SaveCollection(ctx context.Context, c Collection, expected Revision) error. Revision is an opaque content hash.
- Lock covers reread, comparison and rename. Tests must verify the check and write cannot race.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestStoreRoundTrip` — stable IDs survive reload.
- `TestStoreConflict` — stale revision cannot overwrite.
- `TestStoreFutureSchema` — unknown schema is not rewritten.
