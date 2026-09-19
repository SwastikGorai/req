# CLI reference

The executable is `req`. `req help` prints the same high-level usage text;
this page groups the flags by behavior and shows the exact command forms.
Examples are independent snippets.

## Global commands and flags

```text
req init
req collection create|list|rename|delete ...
req folder create|rename|move|delete ...
req request create|list|show|rename|move|delete|edit ...
req tree PATH
req run PATH [flags]
req send METHOD URL [flags]
req env create|list|edit|delete [NAME]
req script edit PATH (--pre|--post)
req import postman FILE [--name NAME] [--strict]
req import postman-env FILE [--name NAME] [--strict]
req import curl --file FILE --save-as PATH [--strict]
req export curl PATH [--resolve --env NAME] [--strict]
```

Global options are normally before the command:

```text
--workspace PATH   use an explicit workspace instead of ancestor discovery
--no-color         disable color output
--help             show usage
--version          print the req version
```

Store-backed commands return a usage/lookup error (2) for invalid paths and a
storage error (7) for malformed,
conflicting or unavailable workspace data.

## Saved-item commands

```sh
req collection create "Example API"
req collection list
req collection rename "Example API" "Renamed API"
req collection delete "Renamed API" --yes

req folder create "Example API/Admin/Reports" --parents
req folder rename "Example API/Admin" "Administration"
req folder move "Example API/Administration/Reports" "Example API/Archive"
req folder delete "Example API/Archive" --yes
req tree "Example API"

req request create "Example API/Auth/Login" --method POST --url '{{base}}/login' --parents
req request list "Example API/Auth"
req request show "Example API/Auth/Login"
req request rename "Example API/Auth/Login" "Sign in"
req request move "Example API/Auth/Sign in" "Example API/Users"
req request delete "Example API/Users/Sign in"
req request edit "Example API/Users/Sign in"
```

`--parents` creates missing folder ancestors but never creates a missing
collection. Collection/folder moves stay within one collection. Names cannot be
empty, contain `/` or `\`, or equal `.`/`..`; duplicate sibling names are
rejected. A nonempty delete needs `--yes` or interactive confirmation.

## Shared request flags

The following flags are accepted by both `send` and `run`, and the structural
request flags are also accepted by `request create` where applicable:

```text
-H, --header NAME:VALUE       append a header entry; repeatable
--query KEY=VALUE             append a query entry; repeatable
--method METHOD               replace the method for this execution
--url URL                     replace the URL for this execution
--body TEXT                   inline raw body
--body-file PATH              exact raw file body
--json JSON|@PATH             JSON body, validated after substitution
--urlencoded KEY=VALUE        URL-encoded field; repeatable
--form KEY=VALUE              literal multipart field; repeatable
--form-file KEY=PATH          multipart file attachment; repeatable
--bearer TOKEN                bearer authentication
--basic-user USER             basic authentication username
--basic-password PASSWORD     basic authentication password
--no-auth                     disable inherited authentication
--env NAME                    select a workspace environment
--var KEY=VALUE               highest-priority execution-local value
--timeout DURATION            positive HTTP deadline, default 30s
--no-follow                   do not follow redirects
--insecure                    disable TLS certificate verification
--fail                        exit 4 when status is >= 400
--output PATH                 atomically save the response body
--raw                         preserve bytes; incompatible with JSON mode
--verbose                     print redacted response headers to stderr
--output-format json          emit one versioned JSON envelope
```

Body modes are mutually exclusive. `--json @PATH` reads a JSON file; inline
JSON is validated after variable substitution. Explicit Content-Type headers
override generated defaults. Repeated query/header flags append entries;
script header `upsert` replaces all case-insensitive matches. Saved `run`
overrides apply to an execution copy and do not rewrite the definition.

Authentication flags are mutually exclusive:

```sh
req send GET https://api.example.test/me --bearer "$API_TOKEN"
req send GET https://api.example.test/me --basic-user alice --basic-password "$API_PASSWORD"
req send GET https://api.example.test/public --no-auth
```

Direct `send` file paths resolve from the current directory. Saved `run` file
paths resolve from the workspace root. Explicit `{{env:NAME}}` is the only
process-environment lookup.

## Run-only script controls

```text
--no-scripts                  skip pre and post scripts
--script-timeout DURATION     per-script deadline, default 5s
--persist-vars                persist eligible environment/collection changes
```

`--persist-vars` is rejected by `send` and requires `--env NAME` when a script
writes the environment. `--no-scripts` is useful with `--output` for large
downloads because it selects the unbounded streaming path.

## Output behavior

Default stdout is the response body only. Status, elapsed time, headers,
script logs, tests and errors are stderr. `--verbose` prints response header
metadata with built-in and configured secret names redacted.

`--output-format json` emits one version-1 envelope containing status code/text,
ordered header entries, duration in milliseconds, body text or base64 plus
encoding, tests, script logs, errors and skipped state. `--raw` conflicts with
JSON. With `--output PATH`, the envelope references the literal path rather
than duplicating the body; the destination is replaced only after a complete
body write when possible.

Pretty JSON response bodies are used only when stdout is a terminal and
`--raw` is absent. Pipes preserve bytes. Bodies buffered for scripts, terminal
JSON or JSON envelopes are capped at 10 MiB; over-limit responses fail before
post scripts see partial content. No-script output-file downloads stream
without that cap.

Examples:

```sh
req run "Example API/Users/List" --output-format json > result.json
req send GET https://example.test/archive.zip --output archive.zip
req run "Example API/Users/List" --raw > response.bytes
req run "Example API/Users/List" --verbose
```

## Exit codes and precedence

```text
0    success, including an explicit script skip
2    usage/config/import validation
3    transport, TLS or deadline failure
4    HTTP >= 400 with --fail
5    script runtime/API or body-limit failure
6    failed pm.test assertion
7    storage conflict or variable-persistence failure
130  user cancellation
```

When outcomes coexist, the effective exit code follows:

```text
130 > 7 > 5 > 6 > 3 > 4
```

JSON output retains the complete error aggregation even when one code wins.

## Editor commands

```sh
req request edit "Example API/Auth/Login"
req env edit local
req script edit "Example API" --pre
req script edit "Example API/Auth/Login" --post
```

The editor is selected from `$EDITOR`, then `$VISUAL`, and is launched without
a shell. Script edits use marker lines (`// ---- req script ID ----`) to keep
ordered entries stable; preserve existing marker IDs and order.
