# Protocol Adapters

English | [简体中文](../protocols.md)

Related: [Documentation Index](./index.md) · [Extensibility](./extensibility.md)

A unified protocol-adapter interface supports WebSocket, SSE, gRPC, and GraphQL in addition to HTTP.

---

## 1. Unified Abstraction

Each protocol implements `ProtocolSession`. Wails binding methods open and close its lifecycle, while Wails events deliver data.

```go
type ProtocolSession interface {
    Open(cfg SessionConfig) (SessionId, error)
    Send(id SessionId, msg OutboundMsg) error
    Close(id SessionId) error
    // Inbound data is sent to the frontend through the `proto:message` Wails event.
}
```

---

## 2. WebSocket (`coder/websocket`)

- Open connections with custom headers, subprotocols, and TLS. Broadcast the `connecting/open/closing/closed` state machine through Wails events.
- Retain sent and received message history for text, binary, ping, and pong frames. Display it as a frontend timeline, allow manual pings, and save message templates.
- Support optional reconnection with exponential backoff, plus message size and rate limits.

---

## 3. SSE (`net/http` Streaming)

- Open a long-lived GET request and incrementally parse `event:`, `data:`, `id:`, and `retry:` fields from `text/event-stream`.
- Reconnect using `retry` and `Last-Event-ID`. Send the event stream to the frontend through Wails events and allow filtering.

---

## 4. gRPC (`grpc-go` + Reflection)

- **Service discovery**: prefer server reflection (`grpc.reflection.v1`) to retrieve service, method, and message descriptors dynamically. When reflection is unavailable, import `.proto` files or a precompiled FileDescriptorSet.
- **Dynamic invocation**: encode and decode from descriptors with `protoreflect` + `dynamicpb`, without pre-generating code for each proto.
- Support all four modes: unary, server-stream, client-stream, and bidi. Streamed messages use Wails events.
- Metadata including auth, deadlines, TLS, and proxy settings reuse the HTTP channel's credential and proxy configuration.

---

## 5. GraphQL

- Model GraphQL as a specialized HTTP body (`body.kind='graphql'`) and POST `{query, variables}`.
- **Introspection**: send the standard introspection query, cache the schema, and use it for editor completion and documentation browsing.
- Support query, mutation, and subscription. Subscriptions use WebSocket transport and reuse section 2.
