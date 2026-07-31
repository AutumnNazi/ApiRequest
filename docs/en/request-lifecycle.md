# Request Lifecycle, Variables, Scripts, and the HTTP Engine

English | [简体中文](../request-lifecycle.md)

This document covers the complete flow from initiating a request through persistence, variable resolution and templates, the script execution model, and HTTP engine internals.

Related: [Documentation Index](./index.md) · [Data Model](./data-model.md) · [Authentication](./auth.md) · [API Contract](./api-contract.md)

---

## 1. Request Lifecycle (One Complete Send)

```text
1. Collect context          -> merge variable scopes and load the Cookie Jar
2. Run pre-request script   -> execute preRequestScript in a goja sandbox;
                              the script can read/write variables and modify the outgoing request
3. Resolve variables        -> replace {{var}} in URL/Header/Body
4. Build native request     -> apply auth, encode body, and set headers
5. Send through HTTP engine -> execute with net/http and collect phase timings
6. Receive response         -> stream the body, parse cookies, and update the Jar
7. Run test script          -> execute testScript with pm.test/pm.expect assertions
8. Persist result           -> write History and return response + test results to the frontend
```

Each phase sends progress (sending/first byte/completed) through Wails events using `runtime.EventsEmit` and frontend `EventsOn`. Requests can be canceled.

---

## 2. Variable Resolution and Template Engine

### 2.1 Resolution Algorithm

The `{{var}}` syntax supports nested and dynamic variables. Resolution runs in Go after scripts execute and before the native request is built:

```text
1. Build the scope chain (Map<name, value>), applying overrides from low to high priority:
   global -> collection (including inherited values) -> environment -> data-file -> local/override
2. Scan the target string for {{ ... }} tokens
3. For each token:
   - starts with $ -> dynamic variable; call its generator
     ($guid/$timestamp/$randomInt/$randomEmail/...)
   - otherwise -> look up the scope chain
4. Support one level of indirect reference by resolving a value containing {{x}} again;
   enforce a maximum depth to prevent cycles
5. Undefined variable: preserve it or report an error, according to configuration,
   and highlight it in the UI
```

### 2.2 Dynamic Variables (Built-In Generators)

`$guid`, `$uuid`, `$timestamp`, `$isoTimestamp`, `$randomInt`, `$randomUUID`, `$randomEmail`, `$randomFirstName`, `$randomIP`, `$randomBoolean`, and others follow Postman's `{{$...}}` naming to reduce migration friction.

### 2.3 Resolution Targets

Apply variable substitution to URLs, query parameters, header keys and values, textual portions of raw/urlencoded/formdata bodies, and auth parameters. **Do not apply template substitution to binary bodies.** File paths are eligible for substitution, but file contents are not.

---

## 3. Script Engine Execution Model

Run sandboxed JavaScript with `goja`, a pure-Go JS engine. Inject a `pm` object implementing a common subset of the Postman API. Script variable mutations are returned through binding results and committed centrally by Go to avoid races.

### 3.1 Execution Sequence

```go
// Inside SendRequest:
ctx := collectContext()                     // Variable scopes + cookies.
// -- Pre-request script --
sandbox := script.NewSandbox(5 * time.Second)
sandbox.InjectPM(ctx, request)              // Inject the pm object.
sandbox.Eval(preScript)                     // Script may mutate the request/set variables.
request = sandbox.MutatedRequest()
ctx = sandbox.MutatedCtx()                  // Collect variable changes.
// -- Send --
resolved := template.Resolve(request, ctx)
resp := httpengine.Send(resolved)
// -- Test script --
sandbox.InjectResponse(resp)
sandbox.Eval(testScript)                    // pm.test / pm.expect
results := sandbox.TestResults()
persistVariableChanges(ctx)                 // Commit centrally in Go to avoid races.
```

### 3.2 Initial `pm.*` API Mapping

| Postman API | Implementation | Priority |
|-------------|----------------|----------|
| `pm.environment.get/set/unset` | Bridge to Go variable scopes; buffer changes and return them | Required |
| `pm.variables.get/replaceIn` | Same bridge, read-only merged view | Required |
| `pm.collectionVariables.*` | Same as above | Required |
| `pm.request.*` | Expose a mutable request object to pre-request scripts | Required |
| `pm.response.json()/.text()/.code/.headers` | Read-only response in test scripts | Required |
| `pm.test(name, fn)` | Collect assertion results | Required |
| `pm.expect` | Inject a compact chai BDD assertion subset | Required |
| `pm.sendRequest(req, cb)` | Call back into the Go HTTP engine through a controlled channel | Secondary |
| `pm.cookies.*` | Bridge to the Cookie Jar | Secondary |
| `console.log/warn/error` | Collect into `scriptLogs` returned to the frontend | Required |

### 3.3 Sandbox Constraints

- No `require`/`import`, no `fs`, and no direct `fetch`. Network access is available only through the controlled `pm.sendRequest` channel.
- Enforce both CPU-time and wall-clock timeouts, plus a memory limit. Scripts within one request are isolated.
- Create a new context for each execution so global state cannot leak across requests.

Script compatibility is a major risk because the Postman `pm.*` surface is large. Cover the high-frequency subset first, expand as needed, and document the supported range.

---

## 4. HTTP Engine Internals

The engine is a focused execution unit: `resolved request -> response result + timings`. It does not know about collections, variables, or other higher-level concepts.

### 4.1 Why Requests Run in Go

- **Bypass CORS**: `fetch` in a WebView is subject to same-origin policy, while a Postman-like tool must call arbitrary targets. Go's `net/http` sends native requests directly.
- **Full network control**: custom and client TLS certificates, proxies, redirect policy, original header order, and timeouts.
- **Accurate timing**: collect DNS, connect, TLS handshake, TTFB, and download durations in the native layer.
- **Stream large responses**: avoid placing a complete large body into the WebView at once.

### 4.2 Phase Timing Collection

Go's standard `net/http/httptrace` package directly exposes DNS/connect/TLS/TTFB hooks, so no third-party wrapper is required:

- Use `httptrace.ClientTrace` callbacks (`DNSStart/DNSDone`, `ConnectStart/ConnectDone`, `TLSHandshakeStart/TLSHandshakeDone`, and `GotFirstResponseByte`) with `time.Now()` markers to collect DNS/connect/TLS/TTFB/download durations.
- Preferred path: inject `httptrace.WithClientTrace` into the request `context`, then stream the body while recording the first byte and completion times.
- If precision is insufficient, fall back to instrumentation around a custom `http.Transport` using `DialContext`/`DialTLSContext` so every `Timing` field remains available.

### 4.3 Required Capabilities

- **Preserve original header order and casing**: Go's `http.Header` is a map, so keys are canonicalized and order is lost. Where required, construct headers through a custom `http.Transport` or ordered representation and disable automatic sorting. This matters for some signature-based APIs.
- **Redirect policy**: configure maximum redirects, whether to follow, and whether cross-origin redirects retain Authorization.
- **Compression**: automatically decode gzip/br/deflate while retaining the original `Content-Encoding` for display.
- **Proxy**: system/manual/PAC (later)/bypass list, supporting HTTP, HTTPS, and SOCKS5.
- **TLS**: use Go's `crypto/tls`; support custom CAs, client certificates (mTLS), and optional verification disablement with a strong warning.
- **Streaming and cancellation**: read bodies in chunks and avoid inlining above the threshold. Cancel through `context.Context` and `CancelRequest`; see the [API contract](./api-contract.md#3-current-method-signatures-generated-bindings-are-authoritative).
- **Connection reuse**: use a global `http.Client` with a pooled `http.Transport`; allow per-host configuration.

### 4.4 Large Request and Response Bodies

- Stream binary and multipart files from their file paths instead of loading them into memory. Replayable bodies can be reopened for redirects, Digest retries, and streaming AWS SigV4 hashing.
- Stream large responses into `blobs/`; return a summary and ref first, then let the frontend load segments on demand.
