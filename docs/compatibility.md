# Compatibility notes

## Phase 13 — Postman data import

The importer accepts Postman collection exports whose `info.schema` identifies
v2.1 and basic environment exports with `values`. It preserves item order,
folder/request IDs, disabled entries, supported inherited `none`/basic/bearer
auth, raw/JSON/urlencoded/formdata bodies, and `prerequest`/`test` source.

Imported disabled variables remain in the native file with a disabled marker
but are excluded from interpolation. Imported file attachments are marked
untrusted and therefore require the existing workspace path checks before a
run. Postman URL path variables with a supplied default are resolved to that
default; missing defaults remain `{{name}}` and fail normal variable
resolution rather than being sent as a colon placeholder.

OAuth, API-key, digest and other unsupported authentication, body modes,
malformed entry shapes, and unsupported script listeners are reported with
source paths. Unsupported request authentication/body is retained in import
metadata and blocks execution; script API compatibility remains a runtime
concern for Phase 14. Lenient imports sanitize invalid names/IDs and warn;
strict imports reject those warnings before any file is written. Import never
executes script source.

## Phase 14 — Imported Postman scripts

Collection `prerequest` events and `test` events are stored as native
pre-request and post-response script entries. Their source arrays join with
newlines; IDs, enabled state and Postman provenance remain attached. At run
time, enabled entries execute in collection → outer-folder → inner-folder →
request order. Import remains side-effect free, so these scripts first run
only when the saved request is executed.

| Surface | Phase 14 behavior |
|---|---|
| `pm.variables`, `pm.environment`, `pm.collectionVariables` | Supported existing get/has/set/unset/replaceIn APIs; values remain execution-local. |
| `pm.request` | Supported pre-request mutation; post-response view is read-only. |
| `pm.response`, `pm.test`, `pm.expect` | Supported existing response adapters and assertion subset. |
| `pm.sendRequest` | Supported callback and Promise forms through the bounded owner-loop scheduler. |
| `pm.execution.skipRequest`, `console` | Supported with existing skip, cancellation and log limits. |
| `pm.globals`, `pm.iterationData`, timers, `require`/modules | Not exposed. Obvious source uses are warnings; runtime guards/reference errors remain authoritative and include the imported script location. |

Static import checks intentionally detect only obvious unsupported calls; they
do not claim complete JavaScript analysis. Strict import rejects those warnings
before writing, while lenient import preserves the source and lets the runtime
report dynamically reached unsupported APIs.

## Phase 15 — cURL command import

The cURL importer accepts one `curl` invocation using POSIX-like quoting and
backslash-newline continuations. It maps `-X`/`--request`, `-H`/`--header`,
the supported data and JSON flags, `-F`/`--form`, `-u`/`--user`, `-G`/`--get`,
`-L`/`--location` and `-k`/`--insecure` into one native request. Repeated data
pieces join with `&`; repeated JSON pieces join directly. `-G` appends the
provided data to the URL without re-encoding it.

The importer never invokes a shell, expands variables, follows shell
pipelines/redirections, or reads `@file` attachments. Body and form file
markers remain untrusted references with the cURL source path in import
provenance; they require the existing remap/path checks before execution.
Without `-L`, imported requests explicitly disable redirect following, while
`-k` preserves the requested insecure TLS setting. Unknown flags, multiple
URLs, active shell constructs and unsupported option combinations fail before
the destination request is written. `--data @file` is retained with a warning
because the native file reference cannot reproduce cURL's newline/NUL
stripping; strict mode rejects that warning.

## Phase 16 — cURL export

`req export curl PATH` emits one POSIX-shell-safe command. Headers, URLs,
inline bodies and credentials use single-quote escaping; disabled entries are
omitted. Saved `{{placeholders}}` and body/form file references remain literal
references by default and are never read. `--resolve --env NAME` is explicit
because it substitutes collection/environment values and can expose secrets in
stdout. The selected environment's disabled variables remain unresolved.

Native saved relative body files execute relative to the workspace root.
Export retains the literal saved spelling rather than copying or rebasing the
file, so the emitted cURL command must run from the corresponding base
directory. Imported source references may still need remapping before native
execution, as documented for untrusted attachments.

Enabled inherited or request scripts are never run during export. Lenient
exports warn when scripts, blocked import behavior or another value cannot be
represented; `--strict` rejects the export before writing the command. Native
multipart text uses cURL's `--form-string` so leading `@` and semicolons stay
literal; file fields remain `-F` references. cURL's default redirect behavior
is represented with `-L` only when the saved request follows redirects.

## Phase 17 — Persisted script variables

`req run --persist-vars` is the opt-in path for saving `pm.environment` and
`pm.collectionVariables` mutations. Environment writes require the selected
`--env`; `pm.variables` and `--var` values are always execution-local. Runtime,
transport, body-limit, cancellation and skip outcomes do not persist changes;
assertion failures and `--fail` HTTP-status failures still do when the writes
are otherwise valid. Persistence checks the saved revisions under the workspace
lock and returns exit 7 on a conflict or write failure.

Collection and environment changes are journaled together when both are dirty.
Workspace startup recovers a complete or interrupted pair, and blocks with an
actionable storage error if either target changed outside the journal. Saved
relative attachment files still execute relative to the workspace root; cURL
exports retain literal file spelling and therefore must run from the matching
base directory.
