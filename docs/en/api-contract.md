# Frontend/Backend API Contract (Wails Bindings and Events)

English | [简体中文](../api-contract.md)

Related: [Documentation Index](./index.md) · [Data Model](./data-model.md) · [Frontend Data Flow](./frontend.md) · [Request Lifecycle](./request-lifecycle.md)

---

## 1. Naming and Organization

- Binding methods are exported methods on Go structs and always use **PascalCase**, such as `SendRequest`. Wails-generated TypeScript bindings use the same names. Snake_case forms in historical docs, such as `send_request`, refer to the same method; this document is authoritative.
- Split methods into domain binding structs (`RequestApi`, `NodeApi`, `EnvApi`, `ConvertApi`, `MockApi`, and `RunnerApi`) and register them centrally in `app.go`, avoiding an oversized App struct.
- The frontend always calls through typed wrappers under `frontend/src/ipc/`. Components must not invoke generated `wailsjs/` code directly. See [frontend.md section 3](./frontend.md#3-typed-ipc-wrapper).
- Types reference the [shared type contract](./data-model.md#3-shared-frontendbackend-types-contract). This document lists signatures without duplicating type definitions.

---

## 2. Error Transport

Wails serializes an `error` returned by a Go method into a **string** and rejects the frontend Promise with it. To preserve structured [AppError](./ops.md#2-error-model-and-user-feedback) values:

1. The Go binding layer serializes `AppError` to JSON and returns the JSON as the error text: `{"kind":"network","detail":"...","phase":"","line":null}`.
2. The frontend IPC wrapper catches the rejection and attempts `JSON.parse`. If parsing fails, it wraps the value as `{kind:'unknown', detail: <original string>}`.
3. Components consume only structured `AppError` values and never raw error strings.

Expected domain failures, such as a script assertion failure, do not use the error channel. They are returned as part of a normal result, such as `ResponseResult.testResults`.

---

## 3. Current Method Signatures (Generated Bindings Are Authoritative)

```go
// RequestApi -- request execution
// The frontend creates sendId (UUID) to correlate progress events and cancellation.
SendRequest(sendId string, req model.HttpRequest, ctx model.SendContext) (model.ResponseResult, error)
CancelRequest(sendId string) error

// NodeApi -- collection-tree CRUD (collection/folder/request all use node)
ListNodes(workspaceId string) ([]model.Node, error)
UpsertNode(node model.Node) (model.Node, error)
DeleteNode(nodeId string) error                              // Soft delete: set deleted_at.
MoveNode(nodeId string, newParentId string, sortOrder float64) error

// HistoryApi -- request history
ListHistory(workspaceId string, q model.HistoryQuery) (model.HistoryPage, error) // summaries + opaque cursor
GetHistory(workspaceId string, id string) (model.HistoryDetail, error) // Load detail on demand.
ClearHistory(workspaceId string) error

// RequestApi -- bounded large-response access
GetResponseBlobInfo(blobRef string) (model.ResponseBlobInfo, error)
ReadResponseBlobRange(blobRef string, offset, limit int64) (model.ResponseBlobChunk, error) // max 1 MiB per chunk
SaveResponseBlob(blobRef, destination string) (int64, error)

// SettingsApi / DialogApi -- credential and desktop file capabilities
GetVaultStatus() secrets.Status
UnlockVault(password string) (secrets.Status, error)
LockVault() secrets.Status
OpenFile(title string) (string, error)
OpenDirectory(title string) (string, error)
SaveFile(title, defaultFilename string) (string, error)
ReadTextFile(path string) (string, error) // user-selected regular UTF-8 file, max 32 MiB
```

**Cancellation semantics**: inside `SendRequest`, register a `context.CancelFunc` for every `sendId`. `CancelRequest(sendId)` invokes that function, and the in-flight request ends with `AppError{kind:"network", detail:"canceled"}`. Calling it with an unknown or completed `sendId` is a no-op that returns nil, avoiding race-condition errors.

The signatures above mirror the current Go bindings. `frontend/src/ipc/` is the only recommended frontend entry point; the historical Phase 1 breakdown is kept as an evolution record, not as a list of missing APIs.

---

## 4. Domain Methods

```go
// EnvApi
ListEnvironments(workspaceId string) ([]model.Environment, error)
UpsertEnvironment(env model.Environment) (model.Environment, error)
DeleteEnvironment(envId string) error
SetActiveEnvironment(workspaceId string, envId string) error
GetGlobalVariables(workspaceId string) ([]model.Variable, error)
SetGlobalVariables(workspaceId string, vars []model.Variable) error

// ConvertApi
ImportData(format string, payload string) (model.ImportResult, error)   // Return a preview tree; persist after confirmation.
ExportData(nodeId string, format string) (string, error)
GenerateCode(req model.HttpRequest, target string, opts model.GenOptions) (string, error)

// ExampleApi (response "Save as Example" + Mock data source)
ListExamples(nodeId string) ([]model.Example, error)
UpsertExample(ex model.Example) (model.Example, error)
DeleteExample(exampleId string) error

// MockApi / RunnerApi
StartMockServer(collectionId string, opts model.MockOptions) (model.MockStatus, error)
StopMockServer(collectionId string) error
RunCollection(target model.RunTarget, opts model.RunOptions) (string, error) // Return runId.
CancelRun(runId string) error

// ProtocolApi (WS/SSE sessions; gRPC uses GrpcApi)
OpenSession(cfg model.SessionConfig) (string, error)          // Return sessionId.
SendMessage(sessionId string, msg model.OutboundMsg) error
CloseSession(sessionId string) error
```

Script execution (`run_prerequest_script` / `run_test_script`) is **not exposed separately**. It is embedded in `SendRequest`; see the [execution sequence](./request-lifecycle.md#31-execution-sequence). Add standalone methods later only if scripts need a dedicated debugging workflow.

---

## 5. Events (Go -> Frontend via `runtime.EventsEmit`)

Every event carries a routing key (`sendId`, `sessionId`, or `runId`). The frontend subscribes with `EventsOn` in the IPC layer and dispatches each event to the corresponding tab-local store.

| Event | Payload (TS) | Phase |
|-------|--------------|-------|
| `request:progress` | `{ sendId: string; phase: 'sending'\|'ttfb'\|'downloading'\|'done'; bytesReceived: number; totalBytes: number }` | current |
| `ws:message` | `{ sessionId: string; direction: 'in'\|'out'; kind: 'text'\|'binary'\|'ping'\|'pong'\|'close'; data: string; ts: number }` | Phase 4 |
| `proto:message` | `{ sessionId: string; protocol: 'sse'\|'grpc'; direction: 'in'\|'out'\|'system'; kind: string; data: string; event?: string; eventId?: string; ts: number }` | current |
| `mock:status` | `{ collectionId: string; state: 'running'\|'stopped'; addr?: string }` | Phase 4 |
| `mock:log` | `{ collectionId: string; method: string; path: string; matched?: string; status: number; ts: number }` | Phase 4 |
| `runner:progress` | `{ runId: string; iteration: number; requestName: string; status: 'pass'\|'fail'\|'skip'; done: number; total: number }` | Phase 4 |
| `sync:status` | `{ state: 'idle'\|'syncing'\|'error'; pendingOps: number; detail?: string }` | current |

Events carry only **incremental, lightweight** data. Large payloads, including response bodies and full reports, are fetched on demand through binding methods after the frontend receives a completion event.
