# Phase 6 — Environments and inherited authentication

**Goal of this phase (plain English):** Run the same request in two environments with scoped variables.

**Dependencies:** Phase 5 checkpoint must be `[x] Done`.

## Do this now

1. [ ] Add environment create/list/edit/delete and versioned JSON storage.
2. [ ] Implement CLI > local > environment > collection variable precedence.
3. [ ] Add explicit process-env references and unresolved-variable location errors.
4. [ ] Implement inherited none/basic/bearer auth and explicit-header precedence.
5. [ ] Test environment switching and verify missing variables prevent network calls.

Stuck on any step? See **Design details** below.

## Checkpoint — Phase 6

- **Status:** `[ ] Not started`
- **What was actually done:** Not implemented. Fill in changed files, decisions and deviations when work occurs.
- **Verification:** `go test ./internal/variables` plus the named test cases below; add affected CLI integration tests. Record the actual test names if refined. 
- **Evidence:** Not run. Record command, exit status and observed assertions here.
- **Next step:** Phase 7, task 1: [phase-7-body-files.md](phase-7-body-files.md)
- **Resume cursor if interrupted:** Task 1; replace with exact task/test/file before handing off.

---

## Design details (LLD)

*Reference material — consult if a task is unclear. Before starting, refine function signatures against the completed code; preserve the behavioral contract linked below.*

- internal/variables/scope.go: ResolveString(text string) (string, error); Get/Set/Unset operate on named scopes.
- Single-pass interpolation uses compact JSON for nonstrings; get missing maps to JS undefined later. See specification section 7.
- Full fixed behavior and edge cases: [IMPLEMENTATION.md](../IMPLEMENTATION.md). Record deviations explicitly; a shorter phase file does not remove requirements.

## Test stubs

- `TestVariablePrecedence` — CLI overrides all mutable scopes.
- `TestVariableMissingNoSend` — server counter stays zero.
- `TestAuthInheritance` — nearest explicit auth wins.
