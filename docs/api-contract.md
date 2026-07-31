# 前后端接口约定（Wails 绑定方法与事件）

[English](./en/api-contract.md) | 简体中文

相关文档：[文档索引](./index.md) · [数据模型](./data-model.md) · [前端数据流](./frontend.md) · [请求生命周期](./request-lifecycle.md)

---

## 1. 命名与组织约定

- 绑定方法是 Go struct 的导出方法，命名一律 **PascalCase**（如 `SendRequest`）；Wails 生成的 TS 绑定与 Go 方法同名。历史文档中的 snake_case 写法（`send_request`）均指同一方法，以本篇为准。
- 方法按领域拆分为多个绑定 struct（`RequestApi` / `NodeApi` / `EnvApi` / `ConvertApi` / `MockApi` / `RunnerApi`），在 `app.go` 统一注册，避免单一 App struct 膨胀。
- 前端一律经 `frontend/src/ipc/` 的 typed wrapper 调用，组件层禁止裸调 `wailsjs/` 生成代码（见 [frontend.md §3](./frontend.md#3-ipc-typed-wrapper)）。
- 类型均引用[共享类型契约](./data-model.md#3-前后端共享类型契约)，本篇只列签名不重复类型定义。

---

## 2. 错误传递约定

Wails 会把 Go 方法返回的 `error` 序列化为**字符串**作为 Promise rejection 抛给前端。为保住结构化错误（[AppError](./ops.md#2-错误模型与用户反馈)），约定：

1. Go 绑定层统一把 `AppError` JSON 序列化后作为 error 文本返回：`{"kind":"network","detail":"...","phase":"","line":null}`。
2. 前端 ipc wrapper 捕获 rejection，尝试 `JSON.parse`；解析失败则包装为 `{kind:'unknown', detail: <原始字符串>}`。
3. 组件层只消费结构化 `AppError`，不接触裸字符串。

业务上"可预期的失败"（如脚本断言未通过）不走 error，作为正常返回值的一部分（`ResponseResult.testResults`）返回。

---

## 3. 当前方法签名（以生成 binding 为准）

```go
// RequestApi —— 请求执行
// sendId 由前端生成（UUID），用于关联进度事件与取消
SendRequest(sendId string, req model.HttpRequest, ctx model.SendContext) (model.ResponseResult, error)
CancelRequest(sendId string) error

// NodeApi —— 集合树 CRUD（collection/folder/request 统一为 node）
ListNodes(workspaceId string) ([]model.Node, error)
UpsertNode(node model.Node) (model.Node, error)
DeleteNode(nodeId string) error                              // 软删除（置 deleted_at）
MoveNode(nodeId string, newParentId string, sortOrder float64) error

// HistoryApi —— 历史
ListHistory(workspaceId string, q model.HistoryQuery) (model.HistoryPage, error) // summary + opaque cursor
GetHistory(workspaceId string, id string) (model.HistoryDetail, error) // 按需拉取 detail
ClearHistory(workspaceId string) error

// RequestApi —— 大响应按需读取
GetResponseBlobInfo(blobRef string) (model.ResponseBlobInfo, error)
ReadResponseBlobRange(blobRef string, offset, limit int64) (model.ResponseBlobChunk, error) // 单块最多 1 MiB
SaveResponseBlob(blobRef, destination string) (int64, error)

// SettingsApi / DialogApi —— 凭据与桌面文件能力
GetVaultStatus() secrets.Status
UnlockVault(password string) (secrets.Status, error)
LockVault() secrets.Status
OpenFile(title string) (string, error)
OpenDirectory(title string) (string, error)
SaveFile(title, defaultFilename string) (string, error)
ReadTextFile(path string) (string, error) // 仅用户选择的普通 UTF-8 文件，最大 32 MiB
```

**取消语义**：`SendRequest` 内部为每个 `sendId` 注册一个 `context.CancelFunc`；`CancelRequest(sendId)` 触发 cancel，进行中的请求以 `AppError{kind:"network", detail:"canceled"}` 结束；对未知/已完成的 `sendId` 调用是 no-op（返回 nil），避免竞态报错。

上述签名来自当前 Go binding；`frontend/src/ipc/` 是唯一推荐的前端入口。历史上的 Phase 1 任务分解保留作项目演进记录，不再代表未实现接口。

---

## 4. 领域方法

```go
// EnvApi
ListEnvironments(workspaceId string) ([]model.Environment, error)
UpsertEnvironment(env model.Environment) (model.Environment, error)
DeleteEnvironment(envId string) error
SetActiveEnvironment(workspaceId string, envId string) error
GetGlobalVariables(workspaceId string) ([]model.Variable, error)
SetGlobalVariables(workspaceId string, vars []model.Variable) error

// ConvertApi
ImportData(format string, payload string) (model.ImportResult, error)   // 返回预览树，确认后再落库
ExportData(nodeId string, format string) (string, error)
GenerateCode(req model.HttpRequest, target string, opts model.GenOptions) (string, error)

// ExampleApi（响应"保存为示例" + Mock 数据源）
ListExamples(nodeId string) ([]model.Example, error)
UpsertExample(ex model.Example) (model.Example, error)
DeleteExample(exampleId string) error

// MockApi / RunnerApi
StartMockServer(collectionId string, opts model.MockOptions) (model.MockStatus, error)
StopMockServer(collectionId string) error
RunCollection(target model.RunTarget, opts model.RunOptions) (string, error) // 返回 runId
CancelRun(runId string) error

// ProtocolApi（WS/SSE 会话；gRPC 使用 GrpcApi）
OpenSession(cfg model.SessionConfig) (string, error)          // 返回 sessionId
SendMessage(sessionId string, msg model.OutboundMsg) error
CloseSession(sessionId string) error
```

脚本执行（`run_prerequest_script` / `run_test_script`）**不单独暴露**，内嵌在 `SendRequest` 流程中（见[请求生命周期](./request-lifecycle.md#31-执行时序)）；后期若需"单独调试脚本"再补独立方法。

---

## 5. 事件（Go → 前端，经 `runtime.EventsEmit`）

事件统一携带路由键（`sendId` / `sessionId` / `runId`），前端在 ipc 层 `EventsOn` 订阅后分发到对应标签页的局部 store。

| 事件 | Payload（TS） | 阶段 |
|------|--------------|------|
| `request:progress` | `{ sendId: string; phase: 'sending'\|'ttfb'\|'downloading'\|'done'; bytesReceived: number; totalBytes: number }` | current |
| `ws:message` | `{ sessionId: string; direction: 'in'\|'out'; kind: 'text'\|'binary'\|'ping'\|'pong'\|'close'; data: string; ts: number }` | Phase 4 |
| `proto:message` | `{ sessionId: string; protocol: 'sse'\|'grpc'; direction: 'in'\|'out'\|'system'; kind: string; data: string; event?: string; eventId?: string; ts: number }` | current |
| `mock:status` | `{ collectionId: string; state: 'running'\|'stopped'; addr?: string }` | Phase 4 |
| `mock:log` | `{ collectionId: string; method: string; path: string; matched?: string; status: number; ts: number }` | Phase 4 |
| `runner:progress` | `{ runId: string; iteration: number; requestName: string; status: 'pass'\|'fail'\|'skip'; done: number; total: number }` | Phase 4 |
| `sync:status` | `{ state: 'idle'\|'syncing'\|'error'; pendingOps: number; detail?: string }` | current |

约定：事件只推**增量/轻量**数据；大 payload（响应体、报告全文）由前端拿到完成事件后经绑定方法按需拉取。
