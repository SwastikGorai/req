# Phase 5 — Editing and organizing requests

**Goal of this phase (plain English):** Edit, rename, move and delete definitions without losing concurrent changes.

**Dependencies:** Phase 4 checkpoint must be `[x] Done`.

## Do this now

1. [x] Implement rename and same-collection move with descendant-cycle checks.
2. [x] Implement request deletion and confirmation for nonempty folder/collection deletion.
3. [x] Launch configured editor without shell evaluation and validate the result.
4. [x] Preserve invalid edits in a recovery file and detect stale revision on save.
5. [x] Test move cycles, noninteractive deletion and editor conflicts.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 5

- **Status:** `[x] Complete`
- **What was actually done:** Implemented in the orchestrator/`subagent-implementer` split (Task A: store; Task B: CLI; plus two orchestrator-initiated design follow-ups when review found real gaps). `internal/store`: `ErrBadMove` sentinel; `SaveRecovery(id, data)` extracted from `conflict()` as the public recovery writer (same `.req/recovery/<id>-<hash>.json`, 0600); `mutations.go` gained `RenameCollection` (name field only; ID/file unchanged; duplicate across collections → `ErrDuplicateName`; same-name no-op), `DeleteCollection(name, expected Revision)` (lock → reread → hash compare → `os.Remove`; stale/vanished → `*ConflictError` without recovery), `RenameItem` (in place, ID/position kept; sibling duplicate → `ErrDuplicateName`; same-name no-op), `MoveItem` (dest = existing folder or collection root; item keeps name/ID, appended LAST at destination; same-parent no-op; cross-collection / under-a-request / into-itself-or-descendant → `ErrBadMove`; cycle = destination path having the source item segments as prefix, checked after the same-parent no-op), `DeleteItem` (folder removes its subtree; single-segment path → `ErrInvalidPath` pointing at collection delete), `UpdateRequest(path, req, expected Revision)` (replaces the request payload in place; persists only if the file is still at the caller's source revision — the check is `SaveCollection`'s, under the workspace lock; stale → `*ConflictError` with the candidate preserved in recovery). Shared walk helpers `locateItem`/`resolveFolderChildren`/`sameSegments`. `internal/cli`: `usageOrStorage` also maps `ErrBadMove` → 2; `collection rename|delete` and `folder rename|move|delete` and `request rename|move|delete|edit` handlers (shared `renameItem`/`moveItem` shapes); confirmation machinery `stdinIsTerminal`/`confirmDelete`/`requireDeleteApproval` (injectable; nonempty without `--yes`: terminal y/N prompt — declined/EOF prints "deletion aborted", exit 0, nothing deleted; non-terminal → exit 2 pointing at `--yes`; empty targets and requests delete freely; `--yes` accepted on `request delete` for symmetry); `edit.go` — `request edit PATH` seeds a `req-edit-*.json` temp with the request JSON (the exact `request show` shape), resolves `$EDITOR` then `$VISUAL` (blank = unset; both unset → instructions, exit 2), splits the value into argv and launches WITHOUT a shell (injectable `runEditor`), short-circuits unchanged edits with no write, strict-decodes (`DisallowUnknownFields` + trailing-data rejection), preserves invalid edits via `ws.SaveRecovery` (exit 2), saves through `UpdateRequest` with the source revision (conflict → 7 with the store's recovery path), removes the temp on every completed path (kept when the editor itself fails or nothing else preserved the edit). `root.go` usage lists the nine new subcommands.
- **Deviations/refinements:** (1) `UpdateRequest` originally saved against its own fresh load, which would have let an editor session silently overwrite a competing save; caught in Task B review, fixed store-side by taking `expected Revision` (under-lock check in `SaveCollection`) and dropping Task B's interim CLI-side pre-check — no check-then-save window remains. (2) `requireDeleteApproval` returns `(proceed bool, exitCode int)` — a single int cannot distinguish "proceed" from "declined (exit 0)". (3) Whitespace-split editor argv means editor arguments containing spaces are not expressible (the price of the spec's no-shell launch); wrap such editors in a script. (4) `request edit` edits only the `request` payload — `name`/`id` are managed by `rename`/`move`. See docs/decisions.md Phase 5 section.
- **Verification:** `go vet ./...`; `go test -count=1 ./...`; `go test -race -count=1 ./...`; `go build ./...`; `gofmt -l internal/` (now empty repo-wide after a separate hygiene commit); manual binary smoke.
- **Evidence:** All commands exit 0; full `-race` suite `ok` (cli/httpclient/model/scripting/store). Store new: `TestRenameItemAndCollection` (rename in place, IDs survive, old path ErrNotFound, duplicate → `ErrDuplicateName`, same-name no-op leaves file bytes identical; collection rename changes the name field only, duplicate across collections rejected), `TestMoveItem` (subtree intact, ID kept, last child at destination, removed from old parent; cross-collection/under-request → `ErrBadMove`; missing dest → `ErrNotFound`; duplicate at destination → `ErrDuplicateName`), `TestMoveItemCycle` (into itself and into a descendant → `ErrBadMove`; same-parent → nil, bytes unchanged), `TestDeleteItemAndCollection` (subtree removal; stale-revision delete → `*ConflictError`, file survives), `TestUpdateRequest` (payload replaced, ID/name/position kept; folder path error; invalid payload leaves file untouched; stale revision → `*ConflictError` + recovery file + competitor's content survives). CLI new: `TestMoveCycleRejected`, `TestDeleteNoninteractive` (non-tty without `--yes` → 2 + `--yes` hint; `--yes` deletes; interactive confirm true/false paths; nonempty collection gate), `TestEditorConflictRecovery` (competing save inside the fake editor → exit 7, store recovery file holds the losing edit, competitor's content on disk), `TestRenameCommands`, `TestMoveCommands`, `TestDeleteCommands`, `TestRequestEditHappyPath` (incl. VISUAL fallback), `TestRequestEditUnchanged`, `TestRequestEditInvalidJSON` (garbage and unknown-field both preserved in recovery), `TestEditorMissingEnv`, `TestEditorFails` (temp preserved and named). Manual smoke of the built binary: collection rename followed by tree; duplicate sibling rename → 2; move appends last under destination; cycle move → 2 with itself/descendant message; nonempty folder delete without `--yes` prompted and aborted on EOF (exit 0, nothing deleted) and `--yes` deleted with the child count; a REAL no-shell editor launch via `EDITOR="sed -i s/GET/PUT/"` applied the edit (show confirmed PUT, exit 0); invalid edit (`sed -i 1s/.*/broken/`) → exit 2 with the edit preserved in `.req/recovery/` and the stored request untouched; both env vars unset → instructions, exit 2; request/collection deletes leave `collection list` empty. Pre-existing gofmt debt (three Phase 0–3 files) fixed in dedicated commit `0f81bdb`; `gofmt -l internal/` is now empty and part of the gate.
- **Next step:** Phase 6, task 1: [phase-6-variables-auth.md](phase-6-variables-auth.md)
- **Resume cursor if interrupted:** Phase 5 complete; start Phase 6 task 1 (environments, variable substitution and inherited auth) against the current working tree.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

**Refined 2026-09-06 against the Phase 4 tree** (`store`: `SplitPath`/`ResolvePath`/`ResolvedPath`, `collectionByName`, `createItem` walk in `mutations.go`, `ListCollections`/`CreateCollection`, sentinels `ErrNotFound`/`ErrInvalidPath`/`ErrDuplicateName`, `SaveCollection(ctx, c, rev)` with `*ConflictError` + recovery files; `cli`: `openWorkspace`, `usageOrStorage`, handlers per command; `request show` prints the item's `request` object as indented JSON):

- **Path grammar unchanged**: collections addressed by unique Name; renames change paths, IDs never change. All mutations persist through `SaveCollection` with the load-time `Revision` (loser gets `*ConflictError`, exit 7).
- `internal/store/mutations.go` (extend) —
  ```go
  // RenameCollection sets the stored collection's Name (file name and ID stay).
  // A name equal to the current one is a no-op; a name used by another
  // collection wraps ErrDuplicateName.
  func (w *Workspace) RenameCollection(ctx context.Context, oldName, newName string) error

  // DeleteCollection removes the collection file under the workspace lock,
  // but only if its current revision still equals expected (stale → *ConflictError).
  func (w *Workspace) DeleteCollection(ctx context.Context, name string, expected Revision) error

  // RenameItem renames the folder/request at path to newName in place
  // (same parent, same ID). newName == current name is a no-op; a sibling
  // already using newName wraps ErrDuplicateName.
  func (w *Workspace) RenameItem(ctx context.Context, path, newName string) error

  // MoveItem moves the folder/request at src under destParent — an existing
  // folder or a collection root — keeping its name and ID; it is appended
  // last among destParent's children. Cross-collection moves, destParent
  // naming a request, and moving a folder into itself or a descendant wrap
  // the new sentinel ErrBadMove; a missing destParent wraps ErrNotFound.
  // Moving under the item's current parent is a no-op (no write).
  func (w *Workspace) MoveItem(ctx context.Context, src, destParent string) error

  // DeleteItem removes the item at path; a folder goes with its whole
  // subtree. The collection root (single-segment path) is not an item —
  // deleting a collection is DeleteCollection.
  func (w *Workspace) DeleteItem(ctx context.Context, path string) error
  ```
  New sentinel in `errors.go`: `ErrBadMove = errors.New("invalid move")`. The CLI pre-validates new names with `model.ValidName` (exit 2 pattern from `collection create`); the store double-checks with a plain error.
- `internal/store` recovery helper (extracted from `conflict`, shared by both) —
  ```go
  // SaveRecovery preserves data under .req/recovery/<id>-<shorthash>.json
  // (0600) and returns its path, so rejected candidates and failed edits
  // survive.
  func (w *Workspace) SaveRecovery(id string, data []byte) (string, error)
  ```
- `internal/cli` — extend `collection.go` (`rename OLD NEW`, `delete NAME [--yes]`), `folder.go` (`rename PATH NEW`, `move SRC DEST`, `delete PATH [--yes]`), `request.go` (`rename PATH NEW`, `move SRC DEST`, `delete PATH [--yes-accepted]`, `edit PATH`), `root.go` usage. Confirmation: a nonempty folder/collection without `--yes` fails in noninteractive use (exit 2, message pointing at `--yes`); when stdin is a terminal it prompts y/N. Injectable for tests: `stdinIsTerminal() bool` and `confirmDelete(prompt string, stderr io.Writer) bool` package vars; a declined prompt prints "deletion aborted" and exits 0 (no deletion). Empty folders/collections and requests delete without confirmation. `request delete` accepts `--yes` for symmetry (no confirmation is needed). Deletion of nonempty means "has any items"; nothing is ever deleted without either the flag or an explicit y.
- `internal/cli/edit.go` (new) — `req request edit PATH`:
  1. Resolve PATH to a request item; capture the collection `Revision`.
  2. Write `json.MarshalIndent(item.Request, "", "  ")` + `\n` to `os.CreateTemp("", "req-edit-*.json")`.
  3. Editor: `$EDITOR` then `$VISUAL` (empty/whitespace values count as unset); neither → setup instructions, exit 2. The value is whitespace-split into argv, the temp path appended, and executed **without a shell** (`exec.Command(argv[0], argv[1:]...)`, os stdio wired). Injectable: `var runEditor = func(argv []string) error`. Nonzero editor exit aborts (exit 2) and keeps the temp file, naming it.
  4. Read back: identical bytes → "unchanged", remove temp, exit 0 (no write). Strict decode (`DisallowUnknownFields`, no trailing data) into `model.Request` — the exact shape `request show` prints; unknown fields are rejected, not discarded. Invalid JSON → preserve the edit via `ws.SaveRecovery(collectionID, editedBytes)`, report the recovery path, exit 2.
  5. Save with `SaveCollection(ctx, updated, capturedRev)`: a stale revision is `*ConflictError` → exit 7 with the store's recovery path (the edit survives there); success prints a status line to stderr, removes the temp.
- **Exit codes**: usage/lookup/duplicate/invalid-name/bad-move/declined-edit → 2; IO/lock/conflict → 7. Editor environment problems → 2. `request edit` edits only the `request` object — `name`/`id` are managed by `rename`/`move` (decided; recorded).
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestMoveCycleRejected` — folder cannot move into itself.
- `TestEditorConflictRecovery` — newer saved file and recovery edit both survive.
- `TestDeleteNoninteractive` — missing --yes prevents destructive folder deletion.
