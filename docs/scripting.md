# Scripting

Saved collections, folders and requests can carry pre-request and
post-response JavaScript. `req` runs the supported subset inside embedded Goja;
it does not require Node.js and does not claim full Postman or Chai
compatibility.

## Attach and order scripts

Use the editor on a collection, folder or request:

```sh
req script edit "Example API" --pre
req script edit "Example API/Auth" --post
req script edit "Example API/Auth/Login" --post
```

Enabled entries run in this order for each phase:

```text
collection -> outer folder -> inner folder -> request
```

The pre phase runs before final variable interpolation and the HTTP request.
The post phase runs after a response is received, including an HTTP error
status. A pre-script runtime error prevents the main request. A failed
`pm.test` assertion records a failure and allows later scripts to continue.
`pm.execution.skipRequest()` skips the main request and all post scripts.

`--no-scripts` skips both phases for one saved run. Export does not execute
scripts; it warns that cURL cannot represent them.

## Supported API surface

| API | Supported behavior |
| --- | --- |
| `pm.variables` | `get`, `set`, `has`, `unset`, `replaceIn`; execution-local scope |
| `pm.environment` | `get`, `set`, `has`, `unset`; selected environment scope |
| `pm.collectionVariables` | `get`, `set`, `has`, `unset`; collection scope |
| `pm.request` | method, URL string, case-insensitive headers, raw/JSON body mutation in pre scripts |
| `pm.response` | code/status, response time, headers, `json()` and `text()` in post scripts |
| `pm.test` | synchronous named callbacks; failures aggregate and continue |
| `pm.expect` | documented `to/be/have/and`, `not`, equality/deep equality, type/property/include/length assertions |
| `pm.response.to.have.status` | numeric response-status assertion |
| `console` | log/info/warn/error with bounded structured logging |
| `pm.sendRequest` | supported string/request object, callback and Promise forms |
| `pm.execution` | `skipRequest()` in pre scripts |

Unknown or unsupported properties fail with a compatibility diagnostic; they
do not return fake successful values. `pm.response` is unavailable in pre
scripts. Environment writes without `--env NAME` fail clearly.

## Request and response examples

Mutate an outgoing header in a pre script:

```javascript
pm.request.headers.upsert("X-Trace", "trace-value");
```

Extract a response token and test it:

```javascript
pm.environment.set("token", pm.response.json().token);
pm.test("token is present", function () {
  pm.expect(pm.response.json().token).to.be.a("string");
});
```

Post scripts can use the response assertion chain:

```javascript
pm.test("created", function () {
  pm.response.to.have.status(201);
  pm.expect(pm.response.json().id).to.be.a("string");
});
```

## Asynchronous auxiliary requests

`pm.sendRequest` runs a supported auxiliary HTTP request without inheriting
the main request's saved headers/auth. Callback errors and unhandled Promise
rejections fail the script; handled network errors may continue. Variables are
resolved when the auxiliary request is scheduled.

Callback form:

```javascript
pm.sendRequest("https://auth.example.test/token", function (err, response) {
  if (err) throw new Error(err);
  pm.variables.set("token", response.json().token);
});
```

Promise form:

```javascript
(async function () {
  const response = await pm.sendRequest("https://auth.example.test/token");
  pm.variables.set("token", response.json().token);
})();
```

The auxiliary scheduler bounds accepted requests, concurrent work and body
size, and each script has a deadline including asynchronous work. Timers,
`require`, modules, filesystem access, shell access and arbitrary Node.js
objects are not exposed. Top-level await is not assumed; use the supported
Promise pattern above.

## Variables and persistence

Variable lookup precedence is:

```text
CLI --var > execution-local pm.variables > selected environment > collection
```

`pm.variables.set/unset` affects only the current execution. Environment and
collection setters affect in-memory overlays. They are written only when a
saved request is run with `--persist-vars` and the outcome is eligible:

- Runtime/script, transport, response body-limit, cancellation and skip stop
  persistence.
- Assertion failures and `--fail` HTTP-status failures still permit otherwise
  valid variable mutations.
- A persistence conflict or write error is exit 7 and outranks script,
  assertion, transport and HTTP-status errors (cancellation 130 still wins).

Environment writes require a selected environment. Direct `send`, CLI values
and local execution variables have no persistence path. A paired collection
and environment write uses the store's small recovery journal.

## Runtime boundaries and limits

Goja runs on one owner goroutine. Host HTTP workers return plain results to the
owner and never touch VM values directly. Supported scripts have deadlines;
auxiliary request/concurrency, decoded response body and console log limits.
The body limit protects script/JSON buffering and prevents post scripts from
seeing partial content. A no-script `--output` download can stream larger
responses.

This is a compatibility/runtime boundary, not a hard heap or security
sandbox. Goja cannot make arbitrary hostile JavaScript safe merely by being
embedded. Review imported scripts, keep sensitive workspaces private, and do
not rely on response-body or script-log redaction. The runtime decision links
the primary [Goja repository](https://github.com/dop251/goja) and
[API documentation](https://pkg.go.dev/github.com/dop251/goja).
