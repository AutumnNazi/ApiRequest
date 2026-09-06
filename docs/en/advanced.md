# Mock Server and Collection Runner

English | [简体中文](../advanced.md)

Related: [Documentation Index](./index.md) · [Request Lifecycle](./request-lifecycle.md)

---

## 1. Mock Server

Start a local HTTP service with Go's `net/http`. The service uses [Examples](./data-model.md#2-database-schema-design) attached to requests in a collection as its data source, matching incoming requests by path and method.

### 1.1 Lifecycle and Ports

- Start and stop servers **per collection** with `StartMockServer(collectionId, opts)`. Each collection owns an independent `http.Server` instance, and state changes are sent to the frontend through `mock:status` events.
- Starting from a fixed base such as 3600, the server probes upward for an available port by default. A port may also be specified in `opts`. After startup, the actual listening address is returned for the UI to display and copy.
- On application exit, call `Shutdown` for every server with a graceful-shutdown timeout. Deleting a collection also stops its mock server.

### 1.2 Matching Algorithm

For each incoming `(method, path)`, score and match all request nodes in the collection in this order:

```text
1. Flatten all requests in the collection and use the path part of each URL as a route template
   (treat {{var}} and :param segments as wildcard segments)
2. Keep requests with the same method; if none match, allow any method and mark the fallback
3. Sort by "more literal segments first, then more total segments" and select the best request
4. Select a response from that request's examples:
   - `x-mock-response-name: <example-name>` header -> select by exact name
   - `x-mock-response-code: <status>` header -> select by status code
   - otherwise select the first example in creation order
5. If nothing matches -> return 404 with a JSON error body listing the closest candidate paths
```

### 1.3 Response Generation

- Return the example's status, headers, and body. Render `{{$...}}` dynamic variables in the body before returning it by reusing the [template engine](./request-lifecycle.md#2-variable-resolution-and-template-engine). Do not resolve regular `{{var}}` placeholders because a mock has no environment context.
- Support an `opts`-level delay, either fixed or a random millisecond range, to simulate slow networks.
- Emit a `mock:log` event for every match so the frontend can build a request-log timeline.
- CORS: return permissive CORS headers for all origins by default, because local frontend development is the typical consumer of a mock server.

---

- **Response scripts (implemented)**: an example can carry a JS snippet (`Example.MockScript`, goja sandbox, fresh runtime per request, 2s watchdog interrupt). Inside the script, a read-only `mockRequest` (`method`/`path`/`query`/`queryFirst(name)`/`headers`/`body`) is available along with `respond({ status, headers, body, delayMs })` to produce the response dynamically; not calling `respond` falls back to the static example, and a thrown error returns 500. The Mock panel's "Response scripts" view doubles as the example manager: name, status, headers, body, and script are editable per example, deletion requires confirmation, and the owning request name is listed (attached by the collection-level query).
## 2. Collection Runner

Execute requests in a collection sequentially and iteratively, with optional CSV/JSON data files driving multiple iterations. Aggregate each request's test results into a run report containing pass/fail status and duration.

### 2.1 Execution Engine

```text
run_collection(target, options):
  iterations = load_data_file()?  // CSV/JSON; one iteration when absent
  for row in iterations:          // inject each row into the data variable scope
    for request in flatten_ordered(target):
      if request.disabled: continue
      result = SendRequest(request, ctx.with_data(row))
      report.push({request, status, tests, duration})
      if options.stopOnError && result.failed: break
  return report  // totals for passed/failed/skipped/duration plus per-request details
```

- **Request disabling (implemented)**: right-click a request in the tree to disable/enable it (disabled requests render struck-through and grayed); the Runner skips disabled requests and counts them as skipped, while single sends are unaffected. Draft or troubleshooting requests can stay in the collection without joining batch runs.
- **Execution order (implemented)**: flatten the tree in display order. Test scripts can call `pm.setNextRequest(name)` to jump or loop within the current iteration row (Postman semantics: no cross-iteration jumps; passing null or an empty string clears the override and unknown names fall back to natural order). Only effective in sequential mode — concurrent load runs execute everything in parallel, where flow control is meaningless. Jumps are capped (200 hops) so self-loops terminate instead of hanging.
- **Data-driven runs**: inject one data-file row into the `data` scope on each iteration. See [variable resolution](./request-lifecycle.md#2-variable-resolution-and-template-engine) for precedence.
- **Concurrency (implemented)**: sequential by default because many APIs have state dependencies. With a concurrency N > 1, (iteration, request) pairs run through a fixed worker pool — data rows still bind per iteration, results sort stably by (iteration, tree order), and StopOnError converges quickly via a cancel channel (in-flight tasks finish; undispatched tasks count as skipped).
- **Request delay (implemented)**: a `delayMs` > 0 inserts a think-time wait between adjacent requests — sequential inserts it between adjacent requests, concurrent inserts it between each worker's tasks; use it for load-test rate control (approaching a target RPS) or polite throttling against rate-limited APIs. The CLI flag is `--delay`.
- **Cancellation**: every run owns a unique `runId`. Cancellation propagates to the active HTTP request and prevents later iterations. Closing an active Runner requires confirmation and cancellation so no background run is orphaned.
- **Reports (implemented)**: export structured results as JSON/HTML for CI. The HTML report is a self-contained single file (inline styles, no external dependencies) rendered through `html/template` contextual escaping, so HTML inside request names or error messages is safely escaped and cannot inject. The CLI mode's exit code reflects the failure count (see the next section).

### 2.2 Headless CLI (apirequest-cli)

`cmd/cli` shares the same core and local database as the desktop app (see [decisions.md](./decisions.md), OPEN-001):

```bash
apirequest-cli run --collection <name|id> [--workspace <name|id>]
  [--data <file>] [--iterations N] [--delay <ms>] [--stop-on-error]
  [--env <name|id>] [--env-file env.json]
  [--report report.json] [--junit junit.xml] [--html report.html] [--db <dir>]
```

- **Environment selection**: `--env` picks a workspace environment by name or id (ambiguous names are rejected); without it the workspace's active environment applies. For CI, prefer `--env-file`: a JSON object `{"KEY": "value"}` where non-string values are rejected with a hint to quote them. Variable precedence: data row > env-file > environment variables > globals.
- **CI integration**: the exit code equals the number of failed requests (capped at 100); 2 means a usage/setup error. `--junit` writes JUnit XML that GitHub Actions test reporting consumes directly; requests skipped by `--stop-on-error` or cancellation count toward the `skipped` attribute and produce no testcase. `--report` writes the full JSON; `--html` writes a self-contained HTML report (open it directly in a browser — good for human archiving and browsing).
- `apirequest-cli list` prints workspaces and collections (with request counts) to discover the `--collection` argument.
- **Headless import (implemented)**: `apirequest-cli import --file <path> [--format <fmt>] [--workspace <name|id>] [--db <dir>]` uses the same formats as the desktop importer (including auto detection and Postman environment files); suggested environments land too (inactive), and stdout carries a JSON summary (collectionId/name/requests/environments) for scripts. Same library and semantics as the desktop app — handy for pipeline assembly.
- **Headless export (implemented)**: `apirequest-cli export --collection <name|id> --format <fmt> [--out file] [--workspace <name|id>] [--db <dir>]` uses the same formats as the desktop exporter (postman/openapi/openapi3.1/swagger2/curl/restclient/har/insomnia); output goes to stdout or an `--out` file (0600) through the same redaction path — secret values never land in the artifact. Useful for folding a local collection into a pipeline or scripted backups as exchange formats.
