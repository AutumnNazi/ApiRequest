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

- **Execution order**: flatten the tree in display order. Scripts may alter control flow with `pm.setNextRequest(name)` in a later phase.
- **Data-driven runs**: inject one data-file row into the `data` scope on each iteration. See [variable resolution](./request-lifecycle.md#2-variable-resolution-and-template-engine) for precedence.
- **Concurrency**: sequential by default because many APIs have state dependencies. Optional bounded concurrency may support load-oriented scenarios.
- **Reports**: export structured results as JSON/HTML for CI. In a later phase, CLI mode uses the failure count as its exit code.
