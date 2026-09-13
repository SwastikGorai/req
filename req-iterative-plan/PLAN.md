# req — Iterative Implementation Plan

> Living document. Start here. Phases 0–8 are implemented and verified. See [verification.md](verification.md) for the 2026-09-12 re-audit, prerequisite corrections and continuation evidence. Next: Phase 9.

## Goal & Context

**What we’re building:** A local-first Go HTTP CLI with saved Postman-style collections/folders, environments, cURL import/export and a supported subset of Postman pre-request and post-response JavaScript, including asynchronous auxiliary requests.

**Why:** Debug and reuse API requests from a terminal without needing a GUI or cloud account.

**Done means:** The native and imported login/profile workflows work across separate CLI invocations, supported scripts/assertions run in order, and every phase has recorded verification evidence.

**Non-goals (explicitly out of scope):** GUI/TUI, cloud sync, collaboration, collection runners, OAuth UI, full Postman/Chai compatibility, Node dependency, arbitrary JS packages, Vault and workflow branching.

*Solo developer or coding agent — lightweight iterations, with verification inside every phase. No deadline is assumed. Full script compatibility work is larger than a weekend; each phase targets one focused sitting, not a guaranteed time estimate.*

## Progress Tracker

| Phase | Name | Status | Depends on | File |
|---|---|---|---|---|
| 0 | Core / Walking Skeleton | [x] Complete | None | [phase-0-core.md](phases/phase-0-core.md) |
| 1 | Embedded JavaScript feasibility | [x] Complete | Phase 0 | [phase-1-runtime-spike.md](phases/phase-1-runtime-spike.md) |
| 2 | HTTP methods, bodies and failures | [x] Complete | Phase 1 | [phase-2-http-options.md](phases/phase-2-http-options.md) |
| 3 | Workspace and atomic persistence | [x] Complete | Phase 2 | [phase-3-storage.md](phases/phase-3-storage.md) |
| 4 | Collections, folders and saved requests | [x] Complete | Phase 3 | [phase-4-saved-requests.md](phases/phase-4-saved-requests.md) |
| 5 | Editing and organizing requests | [x] Complete | Phase 4 | [phase-5-editing.md](phases/phase-5-editing.md) |
| 6 | Environments and inherited authentication | [x] Complete | Phase 5 | [phase-6-variables-auth.md](phases/phase-6-variables-auth.md) |
| 7 | File uploads and body formats | [x] Complete | Phase 6 | [phase-7-body-files.md](phases/phase-7-body-files.md) |
| 8 | Script storage and inherited execution | [x] Complete | Phase 7 | [phase-8-script-lifecycle.md](phases/phase-8-script-lifecycle.md) |
| 9 | Variables, request mutation and response APIs | [ ] Not started | Phase 8 | [phase-9-script-bindings.md](phases/phase-9-script-bindings.md) |
| 10 | Tests and supported assertions | [ ] Not started | Phase 9 | [phase-10-assertions.md](phases/phase-10-assertions.md) |
| 11 | Auxiliary requests with callbacks | [ ] Not started | Phase 10 | [phase-11-async-callbacks.md](phases/phase-11-async-callbacks.md) |
| 12 | Promises, deadlines and cleanup | [ ] Not started | Phase 11 | [phase-12-async-promises.md](phases/phase-12-async-promises.md) |
| 13 | Postman collection and environment import | [ ] Not started | Phase 12 | [phase-13-postman-data.md](phases/phase-13-postman-data.md) |
| 14 | Imported Postman scripts | [ ] Not started | Phase 13 | [phase-14-postman-scripts.md](phases/phase-14-postman-scripts.md) |
| 15 | cURL command import | [ ] Not started | Phase 14 | [phase-15-curl-import.md](phases/phase-15-curl-import.md) |
| 16 | cURL export | [ ] Not started | Phase 15 | [phase-16-curl-export.md](phases/phase-16-curl-export.md) |
| 17 | Persist extracted variables safely | [ ] Not started | Phase 16 | [phase-17-variable-persistence.md](phases/phase-17-variable-persistence.md) |
| 18 | Structured output and downloads | [ ] Not started | Phase 17 | [phase-18-output.md](phases/phase-18-output.md) |
| 19 | Integrated acceptance and handoff | [ ] Not started | Phase 18 | [phase-19-delivery.md](phases/phase-19-delivery.md) |

See [hld.md](hld.md) for High-Level Design (HLD), [IMPLEMENTATION.md](IMPLEMENTATION.md) for the complete behavioral contract, and [AGENTS.md](AGENTS.md) for execution/handoff rules.

## How to resume this project

1. Read this entry point and AGENTS.md. Inspect the repository before touching code.
2. Open the in-progress phase, otherwise the earliest not-started phase. Start at Phase 0 for a new repository.
3. Check prerequisite evidence. Read “Do this now”; use Low-Level Design (LLD) details only as needed.
4. Phases 0 and 1 have concrete initial interfaces. Later phases have bounded tasks, test names and fixed behavior, but their local signatures must be refined against actual code before starting; do not prebuild every future abstraction.
5. Each task should fit roughly 30–60 minutes. If one grows beyond that, split it and its phase before implementing, preserve dependencies and update both trackers.
6. Update actual work, command output and next task in the phase checkpoint; update both trackers before moving on. Keep shared decisions in handoff.md.

## Handoff and document authority

This new folder supersedes the earlier monolithic execution sequence. IMPLEMENTATION.md remains the behavioral reference, with its read-order wording adapted to this folder. AGENTS.md is adapted to the new phase numbering. The original three-file bundle is retained separately.

The checkpoints are the evidence source; a status label alone proves nothing. Use [handoff.md](handoff.md) for cross-phase decisions and [phases/index.md](phases/index.md) for a compact manifest.

## Agent kickoff

> Start with PLAN.md, then AGENTS.md. Implement req phase by phase, beginning at Phase 0 or the earliest incomplete checkpoint. Before each later phase, refine its LLD against the current code without changing the required behavior. Run its verification, record actual evidence and update both trackers before continuing. Complete async scripts and imports as specified; do not stop at the HTTP core. Keep handoff.md sufficient for another agent to resume without chat history.
