# Import and export

Import and export are adapters around the native request model. They parse and
validate before writing, report lossy behavior with source paths, and never
execute imported scripts during import or cURL export.

## Postman collections and environments

Import a v2.1 collection or a basic environment export:

```sh
req import postman ./collection.json --name "Imported API"
req import postman-env ./environment.json --name local
```

The importer preserves supported item order, IDs, folders, variables,
disabled entries, inherited none/basic/bearer auth, raw/JSON/urlencoded/
formdata bodies and `prerequest`/`test` script source. Imported events map to
the native pre-request/post-response phases; see [Scripting](scripting.md).

Use strict mode when any lossy conversion must fail before a collection or
environment is written:

```sh
req import postman ./collection.json --name "Imported API" --strict
req import postman-env ./environment.json --name local --strict
```

Lenient imports preserve supported data and print warnings for unsupported
auth/body forms, invalid names, unsupported script APIs or attachment trust.
Unsupported request auth/body is retained as import metadata and blocks
execution rather than silently sending an unauthenticated request. Obvious
unsupported script calls can be warned at import, but dynamic JavaScript
compatibility is enforced at runtime; static checks are not complete analysis.

Imports are side-effect free: script source is stored but not run, and no
network request should occur during the import command.

## cURL import

Import one supported command through the CLI adapter:

```sh
req import curl --file ./request.curl --save-as "Example API/Users/Create"
req import curl --file ./request.curl --save-as "Example API/Users/Create" --strict
```

The parser handles POSIX-like single/double quoting, escaped characters and
backslash-newline continuations. Supported options include request method,
headers, URL, data/raw/binary/JSON bodies, basic auth, multipart forms,
`-G`, `-L` and `-k`. It does not invoke a shell.

Pipelines, redirections, command substitution, active variable expansion,
unknown flags, multiple URLs and unsupported combinations fail before the
destination request is written. Without `-L`, an imported request explicitly
does not follow redirects; `-k` preserves the insecure TLS choice. `@file`
markers remain untrusted references and may warn where cURL's byte-normalizing
behavior cannot be represented exactly.

## cURL export

Export a saved request as one POSIX-safe command:

```sh
req export curl "Example API/Users/List" > request.curl
req export curl "Example API/Users/List" --strict > request.curl
```

Headers, URLs, credentials and inline bodies use single-quoted shell tokens
with embedded apostrophes escaped safely. Disabled entries are omitted. Saved
`{{placeholders}}` and body/form file references remain literal by default;
files are not opened and scripts are not run.

Enabled inherited or request scripts and other nonrepresentable behavior cause
lenient warnings. `--strict` fails before emitting a misleading command:

```sh
req export curl "Example API/Users/List" --strict
```

To substitute values, explicitly select an environment:

```sh
req export curl "Example API/Users/List" --resolve --env local > resolved.curl
```

`--resolve` can expose credentials in stdout. It does not consult arbitrary
process environment variables; only the selected workspace scope is resolved.
The command fails validation if `--resolve` has no `--env NAME`.

## File-reference bases

Native saved relative body and multipart files execute relative to the
workspace root. Export retains the literal spelling rather than copying or
rebasing a path, so the emitted cURL command must run from the corresponding
base directory. Imported source references are marked untrusted and may need
remapping before native execution. Direct `send` file paths resolve from the
current directory.

## Round-trip example

The supported round trip is a CLI workflow, not just a parser call:

```sh
req request create "Example API/Echo" --method POST \
  --url 'http://127.0.0.1:8080/echo' --query 'q=a b' \
  --header "X-Echo: one 'two'" --body 'name=Ada'
req export curl "Example API/Echo" > echo.curl
req import curl --file ./echo.curl --save-as "Example API/Echo via curl"
req run "Example API/Echo via curl"
```

The acceptance test `TestAcceptanceCurl` uses a loopback server and asserts
the observed method, query, header value and body bytes. Replace the example
host with a real endpoint or local test server; the placeholder address above
is not a public service.
