# Phase 6 — Environments and inherited authentication

**Goal of this phase (plain English):** Run the same request in two environments with scoped variables.

**Dependencies:** Phase 5 checkpoint must be `[x] Done`.

## Do this now

1. [x] Add environment create/list/edit/delete and versioned JSON storage.
2. [x] Implement CLI > local > environment > collection variable precedence.
3. [x] Add explicit process-env references and unresolved-variable location errors.
4. [x] Implement inherited none/basic/bearer auth and explicit-header precedence.
5. [x] Test environment switching and verify missing variables prevent network calls.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 6

- **Status:** `[x] Complete`
- **What was actually done:** Added model/environment.go, store/environment.go, variables/scope.go, cli/environment.go and cli/variables.go. Extended execution preparation, store path resolution and CLI send/run/request creation. Environment CRUD uses strict versioned JSON and revision checks; editor recovery is shared with request editing. Variables resolve once after overrides with explicit process references. Nearest explicit auth wins; request-create stores unresolved auth references; explicit Authorization overrides generated auth. No dependency added.
- **Verification:** `rtk go test -count=1 ./...` passed (89 tests); vet and build passed. Final race/format/diff gate results are recorded in verification.md.
- **Evidence:** TestVariablePrecedence, TestSinglePassVariables, TestEnvironmentStorage, TestEnvironmentValidation, TestEnvironmentSwitchingAndAuthInheritance, TestVariableMissingNoSend, TestEnvironmentEditorAndCRUD. Coverage includes two environments, CLI/process variables, no saved writes, auth inheritance/overrides, malformed JSON, conflict recovery and zero network calls on invalid inputs. The planned TestAuthInheritance is covered by the longer CLI workflow test name.
- **Next step:** Phase 7, task 1: [phase-7-body-files.md](phase-7-body-files.md)
- **Resume cursor if interrupted:** Phase 6 complete; refine Phase 7's body reader/cleanup boundary next.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

- `model.Environment{SchemaVersion, Name, Variables}` stores JSON-compatible values. Environment names must be safe portable filenames (ASCII letters/digits, underscore/hyphen, max 64; Windows device names rejected). Store methods `LoadEnvironment(ctx, name) (Environment, Revision, error)`, `SaveEnvironment(ctx, env, expected) error`, `ListEnvironments(ctx)`, `DeleteEnvironment(ctx, name, expected)` reuse workspace lock, hashes, atomic writer and recovery. Strict decode rejects unknown fields and trailing data; writes use 0600. Editing cannot rename identity.
- Extract the existing request editor's temporary-file/recovery flow for environment editing. Keep `$EDITOR` then `$VISUAL`, quoted argument grouping, no shell, and source-revision checks.
- `variables.Scope` owns CLI, local, environment and collection maps; `Get(name) (any, bool)` resolves precedence; `ResolveString(text, location) (string, error)` substitutes once and rejects remaining references. Only explicit `env:` placeholders call `os.LookupEnv`. Empty/reserved definition names fail validation.
- Extend `execution.Overrides` with optional auth and `execution.Policy` with the execution scope. `Prepare` resolves the merged execution copy, validates JSON after substitution and generates auth only in the absence of an explicit Authorization header. Resolve inherited auth along collection/folder/request before passing the execution copy to Prepare. Do not mutate saved objects.
- Share parsing of `--env`, repeated `--var`, `--bearer`, `--basic-user`, `--basic-password`, `--no-auth` across send/run. Preserve an explicit workspace for direct send with `--env`; standalone send with CLI/process variables needs no workspace. Basic user/password must be supplied together; auth modes are exclusive. CLI variable values are strings and repeated keys use the last value.
- Test storage CRUD/conflicts/invalid names and JSON, environment editor recovery, scope precedence/nonstring values/one-pass/process references, send/run environment switching, inherited auth and overrides, no saved writes, and zero network calls on missing variables or invalid resolved JSON.
- Single-pass interpolation uses compact JSON for nonstrings; get missing maps to JS undefined later. See specification section 7.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestVariablePrecedence` — CLI overrides all mutable scopes.
- `TestVariableMissingNoSend` — server counter stays zero.
- `TestAuthInheritance` — nearest explicit auth wins.
