# 多协议适配器

[English](./en/protocols.md) | 简体中文

相关文档：[文档索引](./index.md) · [可扩展性](./extensibility.md)

统一"协议适配器"接口，除 HTTP 外支持 WebSocket、SSE、gRPC、GraphQL。

---

## 1. 统一抽象

每种协议实现 `ProtocolSession`，生命周期由 Wails 绑定方法打开、Wails 事件推送数据、Wails 绑定方法关闭。

```go
type ProtocolSession interface {
    Open(cfg SessionConfig) (SessionId, error)
    Send(id SessionId, msg OutboundMsg) error
    Close(id SessionId) error
    // 入站数据经 Wails 事件 `proto:message` 推前端
}
```

---

## 2. WebSocket（coder/websocket）

- 打开连接（支持自定义 header、子协议、TLS）；连接状态机：connecting/open/closing/closed，以 Wails 事件广播。
- 收发消息历史留存（text/binary/ping/pong），前端时间线展示，可发送 ping、可保存消息模板。
- 断线重连策略（可选、指数退避）；消息大小与频率有上限保护。

---

## 3. SSE（net/http 流式）

- 以 GET 建长连接，`text/event-stream` 增量解析 `event:`/`data:`/`id:`/`retry:` 字段。
- 断线按 `retry` 与 `Last-Event-ID` 自动续订；事件流以 Wails 事件推前端并可过滤。

---

## 4. gRPC（grpc-go + 反射）

- **服务发现**：server reflection（`grpc.reflection.v1`）动态拉取服务/方法/消息描述；**已支持 .proto 文件直载**（`backend/grpcclient/proto.go`，protoparse 解析源文件，import 目录解析，文件所在目录自动参与）——目标服务器未开启反射时也可离线发现方法，调用阶段仍按需建连。
- **动态调用**：用 `protoreflect` + `dynamicpb` 依描述动态编解码，无需为每个 proto 预生成代码。
- 支持四种模式：unary / server-stream / client-stream / bidi（流式经 Wails 事件推送）。
- metadata（含 auth）、deadline、TLS 与 HTTP 通道复用同一套凭证/代理配置。

---

## 6. GraphQL 订阅（graphql-transport-ws，已实现）

- **会话模型**：一个 `graphql-ws` 协议会话 = 一条订阅。打开时自动完成 `connection_init` / `connection_ack` 握手（10s 超时，`connection_error` 使打开失败），Send 收 `{query, variables?, operationName?}` 并包装为 subscribe 帧。
- **入站解包**：`next` 推为入站数据；`error` 推为错误行；`complete` 推系统关闭通知并自动结束会话；Close 幂等（complete 后连接已自关不算失败）。
- **入口**：GraphQL 面板"订阅"模式（URL + 订阅查询 + Authorization header），事件流面板实时展示。

---

## 5. GraphQL

- 作为 HTTP body 的特化（`body.kind='graphql'`），POST `{query, variables}`。
- **内省**：发标准 introspection query 拉 schema，缓存后供编辑器补全与文档浏览。
- 支持 query/mutation/subscription（subscription 走 WS 传输，复用第 2 节）。
