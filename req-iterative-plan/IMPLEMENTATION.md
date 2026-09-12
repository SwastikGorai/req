# req — Implementation specification

Status: approved behavioral contract. Implementation status and verification belong to PLAN.md and the phase checkpoints.
Version: 1.0 | Prepared: 2026-09-05

## 1. Intent and document authority

Build a local-first Go CLI that covers the everyday Postman workflow: send HTTP requests, organize collections and nested folders, save and execute requests, import Postman collections and cURL commands, and run common Postman pre-request and post-response JavaScript scripts.

Start with PLAN.md, then AGENTS.md; consult this document for the behavioral contract. This document owns product behavior and interfaces; PLAN.md owns sequencing and evidence; AGENTS.md owns agent conduct and handoffs. Explicit user instructions override all three. If two documents conflict, record and resolve the conflict before changing the relevant behavior.

The working binary name is `req`. Deliver a Go application with an embedded JavaScript engine and no required Node.js or cURL installation. This is a defined compatibility subset, not a full Postman clone. Scripts are mandatory for the final release, even though early milestones deliver a useful HTTP client first.

## 2. Scope

Required: direct HTTP; collection/folder/request CRUD; nested folders; JSON persistence; environment variables; basic and bearer auth; raw/JSON/form/multipart bodies; downloads; Postman v2.1 collection and basic environment import; common cURL import/export; inherited pre/post scripts; common assertions; asynchronous auxiliary HTTP; useful human and JSON output.

Deferred: GUI/TUI, cloud sync, shared workspaces, collection runner/iteration data, workflow branching, OAuth token acquisition UI, persistent cookie jars, WebSockets, gRPC, GraphQL-specific behavior, Postman export, package registries, Vault, visualizers, legacy `postman.*`, arbitrary Node.js APIs and full Chai compatibility. GraphQL can still be sent as ordinary HTTP JSON.

## 3. CLI contract

```bash
req init
req send GET https://example.com --query page=1
req send POST https://example.com --json @payload.json
req collection create "My API"
req collection list
req collection rename "My API" "Example API"
req collection delete "Example API" --yes
req folder create "My API/Admin/Reports" --parents
req folder rename "My API/Admin" "Administration"
req folder move "My API/Administration/Reports" "My API/Archive"
req folder delete "My API/Archive" --yes
req tree "My API"
req request create "My API/Auth/Login" --method POST --url '{{base_url}}/login' --json '{"email":"{{email}}"}'
req request list "My API/Auth"
req request show "My API/Auth/Login"
req request edit "My API/Auth/Login"
req request rename "My API/Auth/Login" "Sign in"
req request move "My API/Auth/Sign in" "My API/Users"
req request delete "My API/Users/Sign in"
req run "My API/Auth/Login" --env local --persist-vars
req env create local
req env list
req env edit local
req env delete local
req script edit "My API" --pre
req script edit "My API/Auth" --post
req script edit "My API/Auth/Login" --post
req import postman ./collection.json --strict
req import postman-env ./environment.json --name local
req import curl --file ./request.curl --save-as "My API/Users/Create"
req export curl "My API/Users/List"
```

Examples demonstrate independent commands, not one runnable transcript. PLAN.md defines the coherent acceptance workflow.

Global flags: `--workspace PATH`, `--no-color`, `--help`, `--version`. Resolve workspace from explicit path, otherwise closest ancestor containing `.req`; direct `send` needs no workspace unless using its environment. `init` acts on the current or explicit directory and is idempotent without overwriting configuration.

Shared request flags: `--header/-H`, `--query`, `--method`, `--url`, `--body`, `--body-file`, `--json`, `--form`, `--form-file`, `--urlencoded`, `--bearer`, `--basic-user`, `--basic-password`, `--no-auth`, `--timeout`, `--no-follow`, `--insecure`, `--output`, `--raw`, `--verbose`, `--output-format json`, `--fail`.

Execution flags: `--env`, repeated `--var key=value`, `--no-scripts`, `--script-timeout`, `--persist-vars`. Basic password can be supplied through a variable reference; do not prompt on redirected stdin. All mutually exclusive body and auth modes fail validation. `--json @file` reads JSON from a file; inline `--json` validates after substitution. Empty bodies differ from absent bodies.

Overrides affect an execution copy and do not rewrite the saved request. Apply structural CLI overrides before scripts, allowing scripts to inspect them. CLI variable values remain highest priority throughout execution. Repeated query/header flags append entries; explicit script header upsert replaces all entries with the same case-insensitive name. Document this distinction.

Use `$EDITOR`, then `$VISUAL`, for JSON or script editing, launched with parsed arguments without a shell. If neither is configured, give setup instructions. Validate edited JSON and compare its source revision before replacing; preserve failed edits in a named temporary recovery file.

Move targets are existing folders or collection roots. Keep moves within one collection in v1; reject cross-collection moves clearly. Reject moving a folder inside itself/descendants. Creating missing ancestors requires `--parents`. Nonempty folder/collection deletion requires terminal confirmation or `--yes`; noninteractive deletion without the flag fails.

## 4. Storage contract

```text
.req/
  config.json
  collections/<stable-id>.json
  environments/<safe-name>.json
  .gitignore
```

One collection file holds its complete tree. Collections have `schema_version`, `id`, `name`, `variables`, `auth`, `scripts`, `items`. Each item is a tagged `folder` or `request` with stable ID and name. Folders contain children, optional inherited auth and scripts. Requests contain method, URL, query entries, header entries, auth, body and scripts. Scripts contain ordered `pre_request` and `post_response` arrays with `id`, `source`, `enabled` and optional import provenance. The editor may expose multiple script entries through a documented JSON edit path; the simple script command edits a single combined source only after explicitly preserving separators/order.

Entries are ordered `{key, value, enabled}` lists, preserving duplicates. Variables are named JSON-compatible values; interpolation uses strings directly and compact JSON for nonstrings. Auth is tagged `inherit`, `none`, `bearer` or `basic`; nearest explicit ancestor wins. Explicit Authorization headers take precedence over generated auth. Store secret references rather than resolving them at save time.

Bodies are tagged `none`, `raw`, `json`, `urlencoded`, `multipart`. Raw and JSON have inline text or one file reference, never both. Multipart fields identify text vs file, enabled state, optional content type and filename. Workspace-relative paths resolve from the workspace root; standalone direct request paths resolve from cwd. Imported attachment paths must be confirmed/remapped through editing before accessing untrusted absolute/outside-workspace paths.

Names cannot contain `/`, be empty, or equal `.`/`..`; disallow duplicate sibling names across item types and duplicate collection names. IDs survive rename/move. Imported invalid names receive deterministic suffixes/sanitization and warnings; strict import fails instead. Unknown future schema versions fail without rewriting. Unknown fields in native edited definitions must be rejected rather than silently discarded.

Use restrictive permissions for environment/secret-bearing files where supported. Ignore environments, transient locks, recovery files and local secret configuration; collections may be version-controlled. Never imply gitignore encrypts secrets.

Writes: validate full candidate, acquire workspace mutation lock, reread/check expected content hash, write temp in same directory, flush/close and atomically replace. A lock must cover revision check and replace to avoid a check-then-write race. Use a maintained cross-platform lock implementation or document a verified native approach. Conflicting changes fail with a recovery path; never overwrite blindly. Startup must identify incomplete variable-persistence transactions before proceeding.

`--persist-vars` may change collection and environment together: stage changes under the lock and use a small recovery journal so a crash does not silently produce inconsistent variable state. This is the only required multi-file transaction; avoid adding a database. Routine CRUD changes one collection file.

## 5. Package boundaries

```text
cmd/req                 main, signals, process exit
internal/cli            argument parsing and command adapters
internal/model          definitions and validation
internal/store          discovery, lookup, locks, persistence
internal/variables      scoped views and substitution
internal/httpclient     build/execute HTTP without script knowledge
internal/execution      complete lifecycle and result aggregation
internal/scripting      JS engine, pm bindings, event loop, assertions
internal/importer       Postman and cURL conversion
internal/exporter       cURL output
internal/output         body rendering, summaries, JSON envelope
testdata                synthetic collections/scripts/cURL fixtures
docs                    compatibility, decisions and handoffs
```

CLI handlers call services, not each other. HTTP accepts an execution request and context, returns response metadata plus a managed body stream. Scripting consumes explicit scoped variables/request/response adapters, not the store. The execution service alone decides persistence and error priority. Define narrow interfaces at these boundaries; avoid an interface for every struct.

## 6. HTTP and output semantics

Use `net/http`, context cancellation and a reusable configured transport. Defaults: HTTP deadline 30s, follow at most 10 redirects, TLS verification on, no automatic retries. Allow only http/https. Handle Ctrl+C across main and auxiliary requests. Verify sensitive headers are not forwarded to unrelated origins on redirects. No cURL subprocess execution.

Preserve repeated query values and empty values, avoid double escaping and retain existing URL query components. Validate JSON after substitution; choose an appropriate Content-Type for body mode unless explicitly supplied. Stream file uploads and downloads, closing all streams on every path.

Stdout defaults to body only; stderr contains status, elapsed time, errors, script logs and test summaries. Pretty JSON/color only when stdout is a terminal and `--raw` is absent. `--output` writes body to a file. JSON mode emits one versioned envelope containing status code/text, ordered header entries, duration_ms, body text or base64 plus encoding, tests, script logs, errors and skipped state. With `--output`, JSON references the output path instead of duplicating bytes. Conflicting `--raw` and JSON mode fail.

Script execution buffers at most 10 MiB of the decoded response body, configurable through a documented max-body setting. If the body exceeds the limit, stop/cancel that read, report a body-limit error and do not run post scripts on partial data. Downloads with `--no-scripts` stream without this limit. JSON envelope mode also uses a bounded body buffer. Redact Authorization, proxy authorization, cookies and configured secret header names from diagnostics. Bodies and arbitrary script logging cannot be reliably redacted; document that scripts can print their own values.

Exit codes: 0 success (including explicit script skip), 2 usage/config/import validation, 3 transport/TLS/deadline failure, 4 HTTP >=400 with `--fail`, 5 script runtime/API/body-limit failure, 6 failed assertions, 7 storage conflict/persistence failure, 130 user cancellation. When multiple outcomes coexist use 130 > 7 > 5 > 6 > 3 > 4; preserve all errors in JSON. Command validation occurs before execution and returns 2. HTTP 4xx/5xx still run post scripts.

## 7. Variables and execution lifecycle

Precedence: immutable CLI overrides > execution-local `pm.variables` > selected environment > collection. Explicit `{{env:NAME}}` reads one process environment variable; do not copy the process environment into JS. Reserve `env:` keys against definition collisions. `pm.variables.get/has` resolve the precedence chain; set/unset affect only the execution-local layer. Environment/collection APIs address their own scope. Missing variables fail with location and name. Single-pass interpolation avoids recursion/cycles; unresolved placeholders remaining after substitution fail.

Execution algorithm:

1. Load and validate workspace, request and selected environment; snapshot revisions.
2. Clone request, inherit auth, collect ancestor scripts and apply structural CLI overrides.
3. Construct per-execution variable layers and JS runtime.
4. Run collection, outer-to-inner folder, then request pre scripts sequentially, draining each script's supported async work before advancing.
5. If skipped, omit send/post scripts and return skipped state. If runtime failure, omit send and stop.
6. Resolve the final request including mutations and variables; validate and send.
7. Read response according to output/script buffering policy. On transport failure do not run post scripts. A received HTTP error status is a response, not a transport failure.
8. Run collection, outer-to-inner folder, then request post scripts sequentially.
9. Aggregate tests/logs/errors. Persist changed collection/environment variables only when requested and no runtime/transport/body-limit/cancellation error occurred. Assertion or HTTP-status failures do not roll back otherwise valid variable mutations; skipped executions do not persist. Fail persistence explicitly.
10. Render output and choose exit status; release runtime, streams and locks.

VM global state may be shared between scripts within this one execution; never share across invocations. Scoped variable state is authoritative. Post scripts see the resolved sent request as read-only; pre-script request changes do not modify saved definitions.

## 8. JavaScript compatibility contract

Select an embedded runtime only after Phase 1's spike. Verify maintained versions from primary documentation during implementation and pin dependencies. No Node runtime dependency. A Go JS interpreter is not a security isolation boundary against memory exhaustion: enforce deadlines/task/body/log limits and document the absence of a hard heap sandbox. Never expose filesystem, shell, module loading or unrestricted host objects.

| Surface | Required subset |
| --- | --- |
| `pm.variables` | get, set, has, unset, replaceIn |
| `pm.environment`, `pm.collectionVariables` | get, set, has, unset |
| `pm.request` | method string; url.toString(); headers get/has/add/upsert/remove; body.raw read/write for raw/JSON |
| `pm.response` | code, status, responseTime milliseconds; headers get/has; json(), text() |
| `pm.test` | synchronous named callback; catches assertion errors and continues |
| `pm.expect` | to/be/have/and chain words; equal, deep.equal, a/an, property, include, lengthOf, true/false/null/undefined and not |
| `pm.response.to.have.status` | numeric status assertion |
| `console` | log/info/warn/error, bounded structured logging |
| `pm.sendRequest` | string URL or supported Postman-style request object; callback `(err, response)` or Promise response |
| `pm.execution` | skipRequest in pre phase only |

Specify and fixture-test each assertion's semantics; do not implement loose equality as strict equality accidentally. Unknown pm properties/methods should raise a descriptive compatibility error with phase, source path and line when possible. Do not return fake successful values. `pm.response` is unavailable in pre scripts. Environment writes without a selected environment fail clearly. Missing-key get returns JS undefined; has distinguishes absent from null.

Top-level await is not assumed. Required async fixtures use Promise chains, async IIFEs and callback forms. Async `pm.test` callbacks are deferred and must fail explicitly, not register a passing test before completion. `pm.sendRequest` itself is required before the final release.

The auxiliary request object subset includes url, method, header (entry array or string map), body (raw/urlencoded/formdata) and basic/bearer auth; reuse main request conversion and HTTP defaults. It executes no saved request scripts and has no implicit main-request headers/auth. Variables resolve at scheduling time. Callback errors and unhandled Promise rejections fail the script; a handled network rejection need not fail it.

All JS calls run on one owning event-loop goroutine. HTTP workers return results through a queue and never call VM APIs directly. Track pending callbacks and Promise jobs; finish a script only when all tracked work has settled. Default total deadline per script entry is 5s including async work; default maximum 20 auxiliary requests and 4 concurrent requests per execution; cap logs at 1000 entries/1 MiB with an explicit truncation warning. Auxiliary responses obey the body limit. Late completions after cancellation are discarded safely. Timers and external modules are deferred; expose neither silently.

`skipRequest()` is a control signal: immediately terminate remaining pre scripts, cancel their pending work and omit all post scripts. Runtime exceptions stop the remaining phase. Assertion failures inside pm.test continue subsequent tests and scripts; a pre-script failed assertion alone does not cancel HTTP execution.

## 9. Postman import

Read v2.1 collection JSON and normalize recursively, preserving item order, variables, disabled fields, auth inheritance, body modes and script source. Script events use `listen: prerequest` and `listen: test`; the latter maps to post-response. Preserve string-array source with newline joins, IDs/provenance where available, and enabled state. Never run code during import.

Handle URL strings and structured URLs with raw/components/query without adding the query twice. Resolve path-variable representations within the documented subset; warn/fail unsupported forms rather than altering semantics silently. Import basic environment exports with disabled values preserved. Report counts, source paths and warnings for unsupported auth/scripts/packages/body forms. Do not claim static analysis can find every unsupported JavaScript call: detect obvious features at import and enforce API compatibility at runtime.

Default import creates a uniquely named collection with a warning if needed; `--name` chooses the name and fails on collision. Strict mode rejects any lossy conversion/known unsupported feature before writing. Keep full unsupported source in import metadata for recovery, but block execution of requests whose auth/body cannot be faithfully represented; scripts with unsupported APIs fail at runtime. Do not send an unauthenticated request as a silent fallback for unsupported auth.

## 10. cURL import/export

Parse shell-like POSIX quoting into tokens without a shell. Allow one curl invocation, quoted strings and line continuations. Reject pipelines, redirections, command substitution, variable expansion and multiple commands when syntactically active; literal characters inside single quotes are data. Reject unknown flags and multiple URLs.

Required flags: -X/--request, -H/--header, -d/--data/--data-raw, --data-binary, --json, --url, -u/--user, -F/--form, -G/--get, -L/--location, -k/--insecure. Match supported flag precedence, method inference, repeated data joining, file markers and URL encoding against official documentation and fixtures. Reject combinations whose semantics are not implemented. Map imported redirect defaults faithfully: cURL without -L must not acquire CLI-default redirect following.

Export POSIX-safe quotes and preserve placeholders by default. `--resolve --env NAME` explicitly resolves values and may expose secrets. Export does not run scripts. Warn whenever scripts or other nonrepresentable behavior are omitted; strict export fails rather than producing a misleading equivalent. File references remain references with documented base paths.

## 11. Source references and implementation verification

These official references support the compatibility target; verify exact dependency/API details when implementing rather than treating this plan as complete vendor documentation:

- [Postman scripts and inherited ordering](https://learning.postman.com/docs/tests-and-scripts/write-scripts/intro-to-scripts)
- [Postman sandbox APIs](https://learning.postman.com/docs/tests-and-scripts/write-scripts/postman-sandbox-reference/overview)
- [Postman auxiliary requests](https://learning.postman.com/docs/tests-and-scripts/write-scripts/postman-sandbox-reference/pm-send-request)
- [Postman execution controls](https://learning.postman.com/docs/tests-and-scripts/write-scripts/postman-sandbox-reference/pm-execution)
- [Postman collection schemas](https://schema.getpostman.com/)
- [cURL manual](https://curl.se/docs/manpage.html)
- [Go HTTP package](https://pkg.go.dev/net/http)

Runtime/library choice, module path and dependency versions remain implementation decisions. Any deliberate deviation from Postman must be listed in docs/compatibility.md with a fixture and user-visible behavior.
