package binding

import (
	"context"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"

	"apirequest/backend/protocol"
)

// ProtocolApi 多协议会话域（docs/api-contract.md §4）
type ProtocolApi struct {
	ctx     context.Context
	manager *protocol.Manager
}

// NewProtocolApi 构造
func NewProtocolApi(manager *protocol.Manager) *ProtocolApi {
	return &ProtocolApi{manager: manager}
}

func (a *ProtocolApi) startup(ctx context.Context) { a.ctx = ctx }

// OpenSession 打开 WS/SSE 会话；入站数据经 proto:message 事件推前端。
// sessionId 由前端生成。
func (a *ProtocolApi) OpenSession(sessionId string, cfg protocol.SessionConfig) error {
	emit := func(msg protocol.InboundMsg) {
		if a.ctx != nil {
			wailsrt.EventsEmit(a.ctx, "proto:message", msg)
		}
		// 连接自然关闭时从会话表移除
		if msg.Kind == "close" || msg.Kind == "error" {
			a.manager.Remove(msg.SessionId)
		}
	}
	return a.manager.Open(sessionId, cfg, emit)
}

// SendMessage 发送消息（WS）
func (a *ProtocolApi) SendMessage(sessionId, data string) error {
	return a.manager.Send(sessionId, data)
}

// CloseSession 关闭会话
func (a *ProtocolApi) CloseSession(sessionId string) error {
	return a.manager.Close(sessionId)
}
