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
