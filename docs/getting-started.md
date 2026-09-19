# Getting started

## Install

The module targets Go 1.25. Build a local binary or install the command:

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

## Send a request

Direct requests do not require a workspace:

```sh
req send GET https://httpbin.org/get --query page=1
req send POST https://httpbin.org/anything --json '{"hello":"world"}'
```

The response body is stdout. Status, elapsed time, errors, script logs and
tests are stderr, so the body can be piped cleanly. See the [CLI reference](cli-reference.md)
for request, body, authentication and output flags.

## Create a workspace

Initialize a directory before saving requests:

```sh
mkdir api-workspace
cd api-workspace
req init
req collection create "Example API"
req request create "Example API/Health" --method GET --url https://httpbin.org/get
req run "Example API/Health"
```

`req` discovers the closest ancestor containing `.req`. From elsewhere, select
the workspace before the command:

```sh
req --workspace ./api-workspace collection list
```

Saved paths use slash-separated collection/folder/request names. Create
nested folders with `--parents`:

```sh
req folder create "Example API/Admin/Reports" --parents
req request create "Example API/Admin/Reports/List" \
  --method GET --url 'https://api.example.test/reports'
req tree "Example API"
```

## Add an environment

Environment names use ASCII letters, digits, `_` and `-`, up to 64 characters:

```sh
req env create local
req env edit local
req env list
```

Add values through the editor, then select the environment with `--env local`.
An environment export can be imported instead:

```sh
req import postman-env ./environment.json --name local
```

Variable lookup precedence is `--var KEY=VALUE`, execution-local
`pm.variables`, the selected environment, then collection variables. `--var`
values are not saved. A literal `{{env:NAME}}` reads one named process
environment value; the entire process environment is never exposed to
scripts.

## Login/profile workflow

Create two saved requests and an environment value for the login input:

```sh
req collection create "Accounts API"
req env create local
req request create "Accounts API/Login" --method POST \
  --url 'https://api.example.test/login' \
  --json '{"username":"alice","password":"{{password}}"}'
req request create "Accounts API/Profile" --method GET \
  --url 'https://api.example.test/profile' \
  --header 'Authorization: Bearer {{token}}'
req env edit local
```

Add `password` in the environment editor. Attach a post-response script:

```sh
req script edit "Accounts API/Login" --post
```

Enter:

```javascript
pm.environment.set("token", pm.response.json().token);
pm.test("login returned a token", function () {
  pm.expect(pm.response.json().token).to.be.a("string");
});
```

Run login and profile as separate CLI invocations:

```sh
req run "Accounts API/Login" --env local --persist-vars
req run "Accounts API/Profile" --env local
```

`--persist-vars` explicitly saves eligible environment and collection changes;
it does not save direct-send, `--var` or execution-local values. Treat saved
environment files as secret-bearing data. See [Scripting](scripting.md) for
script ordering and persistence eligibility.

## Import and export

Import a supported Postman v2.1 collection without running its scripts:

```sh
req import postman ./collection.json --name "Imported API"
req run "Imported API/Auth/Login"
```

Use `--strict` to reject warnings before writing. Export a saved request and
round-trip it through cURL:

```sh
req export curl "Accounts API/Profile" > profile.curl
req import curl --file ./profile.curl --save-as "Accounts API/Profile via curl"
req run "Accounts API/Profile via curl"
```

Export preserves placeholders and file-reference spelling by default. Use
`--resolve --env local` only when substituting values is intentional; resolved
output may expose secrets. See [Import and export](import-export.md) and the
[compatibility matrix](compatibility.md) for supported forms and warnings.

## Output and common fixes

```sh
req run "Accounts API/Profile" --output-format json > result.json
req send GET https://example.test/archive.zip --output archive.zip
req run "Accounts API/Profile" --verbose
```

Common fixes:

- **No workspace found:** run `req init` or use `req --workspace PATH ...`.
- **Unresolved `{{name}}`:** define it in the collection/environment or pass
  `--var name=value`; select the environment with `--env NAME`.
- **Saved file cannot be opened:** native relative files resolve from the
  workspace root; imported references may need remapping.
- **Import warning:** inspect the source path and use `--strict` when lossy
  conversion must fail before writing.
- **Script failure:** check the supported API and phase order in
  [Scripting](scripting.md); `pm.response` is post-response only.
