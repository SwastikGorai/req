# req

`req` is a local-first HTTP client for repeatable terminal workflows. It
combines direct HTTP requests, inspectable Postman-style collections,
environments, supported Postman scripts, and shell-free cURL import/export in
one Go binary.

It is designed for API work without a GUI, cloud account, Node.js runtime, or
external cURL process. `req` implements a documented compatibility subset,
not a full Postman clone; see the [compatibility matrix](docs/compatibility.md)
before relying on an imported feature.

## Features

- HTTP/HTTPS requests with query, headers, raw/JSON/form/multipart bodies,
  basic or bearer auth, redirects, TLS, timeouts and streamed downloads.
- Local JSON collections, nested folders, saved requests, environments and
  inherited scripts under `.req/`.
- Postman v2.1 collection/environment import and supported inherited scripts.
- POSIX-safe cURL export with placeholders and file references preserved by
  default, plus shell-free cURL import.
- Embedded Goja scripts with callback/Promise `pm.sendRequest`, assertions and
  explicit persisted-variable runs.
- Body-only stdout, stderr diagnostics, versioned JSON envelopes and atomic
  output paths for pipelines.

## Install and build

The module targets Go 1.25. Build a local binary or install the local command:

```sh
go build -o req ./cmd/req
go install ./cmd/req
```

No Node.js or external cURL executable is required at runtime. Check the
available commands with:

```sh
req help
req --version
```

## Quick start

Direct requests do not require a workspace:

```sh
req send GET https://httpbin.org/get --query page=1
req send POST https://httpbin.org/anything --json '{"hello":"world"}'
```

The response body goes to stdout; status and diagnostics go to stderr. Save a
request in a workspace:

```sh
mkdir api-workspace
cd api-workspace
req init
req collection create "Example API"
req request create "Example API/Health" --method GET --url https://httpbin.org/get
req run "Example API/Health"
```

From elsewhere, select the workspace before the command:

```sh
req --workspace ./api-workspace collection list
```

For a persisted login/profile example, start with
[Getting started](docs/getting-started.md).

## Documentation

- [Getting started](docs/getting-started.md): install, workspace setup,
  environments, first requests, login/profile, imports and troubleshooting.
- [CLI reference](docs/cli-reference.md): commands, request/body/auth flags,
  run-only flags, output modes and exit codes.
- [Scripting](docs/scripting.md): inherited ordering, supported `pm` APIs,
  callback/Promise requests, assertions and variable persistence.
- [Import and export](docs/import-export.md): Postman/cURL forms, strict mode,
  warnings, placeholders and file-reference bases.
- [Compatibility matrix](docs/compatibility.md): supported behavior and
  deliberate limitations with acceptance evidence.
- [Decision log](docs/decisions.md): architecture decisions and the
  source-linked Goja runtime choice.

## Safety essentials

`req` uses Go’s `net/http` and never invokes a shell or external cURL. Scripts
cannot access arbitrary files, spawn processes, load modules or read the
complete process environment. Goja is an embedded interpreter, not a hard
heap/security sandbox; supported execution is bounded by deadlines, task,
body and log limits. Review imported scripts and files before running them.

Environment files and local configuration may contain secrets. Keep them out
of source control; `.gitignore` is not encryption. Verbose and JSON header
metadata redacts Authorization, Proxy-Authorization, Cookie, Set-Cookie and
configured `secret_headers` names. Response bodies and script logs are not
reliably redacted, and `--resolve --env NAME` can expose saved values in
stdout.

## Compatibility at a glance

| Surface | Supported | Not promised |
| --- | --- | --- |
| HTTP | HTTP/HTTPS, standard bodies, redirects, TLS and output streams | Retries, cookie jar, WebSockets, gRPC or a GraphQL-specific client |
| Postman | v2.1 data and documented inherited script APIs | Full Postman/Chai/Node compatibility, modules, timers or arbitrary globals |
| cURL | One shell-free supported command and POSIX-safe export | Pipelines, redirections, shell expansion or every cURL option |
| Storage | Local JSON workspace with revision-checked writes | Cloud sync, collaboration, GUI, collection runner or iteration data |
