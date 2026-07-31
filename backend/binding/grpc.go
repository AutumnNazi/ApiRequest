package binding

import (
	"context"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"

	"apirequest/backend/grpcclient"
)

// GrpcApi gRPC 反射发现与动态调用域（docs/protocols.md §4）
// 同时承载 unary 调用与 streaming 会话。
type GrpcApi struct {
	ctx context.Context
}

// NewGrpcApi 构造
func NewGrpcApi() *GrpcApi { return &GrpcApi{} }

func (a *GrpcApi) startup(ctx context.Context) { a.ctx = ctx }

// GrpcDiscover 经 server reflection 列出服务与方法
func (a *GrpcApi) GrpcDiscover(cfg grpcclient.ConnectConfig) ([]grpcclient.MethodInfo, error) {
	return grpcclient.Discover(cfg)
}

// GrpcCall 动态 unary 调用
func (a *GrpcApi) GrpcCall(cfg grpcclient.ConnectConfig, fullMethod, requestJSON string, headers map[string]string) (*grpcclient.CallResult, error) {
	return grpcclient.Call(cfg, fullMethod, requestJSON, headers)
}

// GrpcStreamOpen 打开一个流式 gRPC 会话；入站消息经 grpc:stream 事件推前端。
// sessionId 由前端生成；服务端在 EOF 或 error 时会自动从会话表移除并触发 kind=done/error。
func (a *GrpcApi) GrpcStreamOpen(sessionId string, cfg grpcclient.ConnectConfig, fullMethod string, headers map[string]string) error {
	// ctx fallback：与 GraphqlApi 行为一致，避免极端场景 a.ctx 尚未注入时静默丢消息。
	// 注意 EventsEmit 必须 Wails 上下文，context.Background() 没法直接用，所以这里仅在 a.ctx 非空时 emit。
	emit := func(msg grpcclient.StreamMessage) {
		if a.ctx != nil {
			wailsrt.EventsEmit(a.ctx, "grpc:stream", msg)
		}
	}
	_, err := grpcclient.OpenStream(cfg, fullMethod, sessionId, headers, emit)
	return err
}

// GrpcStreamSend 向已打开的流发送一条消息（client-stream / bidi 用）
func (a *GrpcApi) GrpcStreamSend(sessionId, jsonPayload string) error {
	return grpcclient.SendStream(sessionId, jsonPayload)
}

// GrpcStreamClose 主动关闭并销毁流会话
func (a *GrpcApi) GrpcStreamClose(sessionId string) error {
	return grpcclient.CloseStream(sessionId)
}

// GrpcStreamCloseSend 仅向 client-stream 半关（CloseSend）；
// 用于 client-stream-only 的"发完即关"语义，让服务端继续回响应。
func (a *GrpcApi) GrpcStreamCloseSend(sessionId string) error {
	return grpcclient.CloseSendStream(sessionId)
}
