# Agent instructions — iterative req plan

## Start here

Read PLAN.md first, this file second, and the active phase file third. Consult IMPLEMENTATION.md for exact product behavior and hld.md for architecture. This folder replaces the old P00–P10 work sequence; use Phase 0–19 throughout.

## Mission and boundaries

Implement the complete specified Go CLI, including imported pre/post scripts and callback/Promise pm.sendRequest. A useful HTTP core is the first checkpoint, not the final scope. No mandatory Node.js/cURL process, cloud account, GUI or collection runner. Do not advertise full Postman compatibility.

## Working loop

1. Inspect repository instructions, git state and user edits before making changes.
2. Find the first incomplete phase and verify prerequisite evidence.
3. For Phase 2 onward, refine local LLD signatures against actual earlier code before implementation. Keep tasks bounded; split oversized tasks/phases and synchronize links if needed.
4. Implement its checklist and named behavioral tests. Use loopback servers and temporary workspaces.
5. Record actual changed files, test commands/results and next task in the phase checkpoint.
6. Update PLAN.md, phases/index.md and handoff.md before moving to the next phase.

## Rules that preserve intent

- Behavior in IMPLEMENTATION.md is authoritative; phase summaries do not remove requirements. Explicit user/higher-level instructions override project files.
- Structural CLI overrides precede scripts; final variable interpolation follows pre scripts.
- Both script phases run collection to nested folders to request. Pre runtime error/skip prevents main HTTP; an HTTP status error still permits post scripts.
- JS runtime interaction stays on its owner goroutine except documented interrupt operations; HTTP callbacks never touch runtime values directly.
- Imported scripts never run on import. Unsupported auth/body behavior must not silently change a sent request.
- No filesystem/shell/module registry or broad process environment access is exposed to JavaScript.
- Preserve atomic write/revision checks, cancellation, body limits, variable persistence eligibility and exit-code precedence.
- Make routine reversible implementation choices independently. Record consequential choices in handoff.md and, once created, docs/decisions.md.
- Do not overwrite unrelated edits, publish/push/deploy remotely or send messages without applicable authorization.

## Handoff procedure

At every pause record repository/branch/commit or working-tree state, active phase/task, changed files, public interfaces, test commands and observed results, unresolved failures, deviations and exact next action. Never label an unrun test as passed. The successor must be able to resume without chat history.

If a mandatory gate is blocked, record the exact error and independent work that can proceed. Ask the user only when a concrete decision changes scope or affects unrelated work. Do not silently drop async compatibility to finish faster.

## Completion

All phases need evidence, compatible examples, README and compatibility matrix. Cross-compilation is not platform runtime testing. Summarize actual coverage and remaining limitations. Delegation is optional only when the active user/platform instructions authorize it; if used, assign disjoint file ownership and retain one integrator for lifecycle/contracts.
