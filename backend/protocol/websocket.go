package protocol

import (
	"context"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"apirequest/backend/model"
)

// wsSession WebSocket 会话（docs/protocols.md §2）
type wsSession struct {
	conn   *websocket.Conn
	cancel context.CancelFunc
	emit   EmitFunc
	id     string
}

// maxWSMessage 单条消息大小上限（保护，docs/protocols.md：消息大小有上限）
const maxWSMessage = 4 << 20 // 4 MiB

func openWebSocket(id string, cfg SessionConfig, emit EmitFunc) (Session, error) {
	header := http.Header{}
	for _, h := range cfg.Headers {
		if h.Enabled && h.Key != "" {
			header.Set(h.Key, h.Value)
		}
	}
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 15*time.Second)
	conn, _, err := websocket.Dial(dialCtx, cfg.Url, &websocket.DialOptions{
		HTTPHeader: header,
	})
	dialCancel()
	if err != nil {
		return nil, model.WrapError(model.KindNetwork, err)
	}
	conn.SetReadLimit(maxWSMessage)

	ctx, cancel := context.WithCancel(context.Background())
	s := &wsSession{conn: conn, cancel: cancel, emit: emit, id: id}

	emit(InboundMsg{
		SessionId: id, Protocol: "websocket", Direction: "system",
		Kind: "open", Data: cfg.Url, Ts: time.Now().UnixMilli(),
	})

	// 读循环：入站消息推事件；连接关闭时通知
	go func() {
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				kind, detail := "close", err.Error()
				if status := websocket.CloseStatus(err); status != -1 {
					detail = status.String()
				}
				emit(InboundMsg{
					SessionId: id, Protocol: "websocket", Direction: "system",
					Kind: kind, Data: detail, Ts: time.Now().UnixMilli(),
				})
				return
			}
			kind := "text"
			payload := string(data)
			if typ == websocket.MessageBinary {
				kind = "binary"
				payload = base64Encode(data)
			}
			emit(InboundMsg{
				SessionId: id, Protocol: "websocket", Direction: "in",
				Kind: kind, Data: payload, Ts: time.Now().UnixMilli(),
			})
		}
	}()
	return s, nil
}

func (s *wsSession) Send(data string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.conn.Write(ctx, websocket.MessageText, []byte(data)); err != nil {
		return model.WrapError(model.KindNetwork, err)
	}
	s.emit(InboundMsg{
		SessionId: s.id, Protocol: "websocket", Direction: "out",
		Kind: "text", Data: data, Ts: time.Now().UnixMilli(),
	})
	return nil
}

func (s *wsSession) Close() error {
	s.cancel()
	return s.conn.Close(websocket.StatusNormalClosure, "client closed")
}

func init() { registerOpener("websocket", openWebSocket) }
