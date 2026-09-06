// graphql-ws 会话（docs/protocols.md §6）：graphql-transport-ws 子协议订阅。
// 一个会话 = 一个订阅操作：打开即完成 connection_init/ack 握手，Send 收
// {query, variables?, operationName?}（其余字段忽略）并发起 subscribe，
// 入站 next/error/complete 解包后推前端；complete 后会话自动关闭。
package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"apirequest/backend/model"
)

const (
	gqlHandshakeTimeout = 10 * time.Second
	gqlSubprotocol      = "graphql-transport-ws"
)

type graphqlWsSession struct {
	conn   *websocket.Conn
	cancel context.CancelFunc
	emit   EmitFunc
	id     string
}

func openGraphQLWs(id string, cfg SessionConfig, emit EmitFunc, client *http.Client) (Session, error) {
	header := http.Header{}
	for _, h := range cfg.Headers {
		if h.Enabled && h.Key != "" {
			header.Set(h.Key, h.Value)
		}
	}
	header.Set("Sec-WebSocket-Protocol", gqlSubprotocol)
	dialCtx, dialCancel := context.WithTimeout(context.Background(), 15*time.Second)
	conn, _, err := websocket.Dial(dialCtx, cfg.Url, &websocket.DialOptions{
		HTTPHeader:   header,
		HTTPClient:   client,
		Subprotocols: []string{gqlSubprotocol},
	})
	dialCancel()
	if err != nil {
		return nil, model.WrapError(model.KindNetwork, err)
	}
	conn.SetReadLimit(maxWSMessage)

	ctx, cancel := context.WithCancel(context.Background())
	s := &graphqlWsSession{conn: conn, cancel: cancel, emit: emit, id: id}

	emit(InboundMsg{
		SessionId: id, Protocol: "graphql-ws", Direction: "system",
		Kind: "open", Data: cfg.Url, Ts: time.Now().UnixMilli(),
	})

	// 握手：connection_init → 等待 connection_ack（错误消息同样使打开失败）
	initFrame, _ := json.Marshal(map[string]string{"type": "connection_init"})
	hsCtx, hsCancel := context.WithTimeout(ctx, gqlHandshakeTimeout)
	defer hsCancel()
	if err := s.writeFrame(hsCtx, initFrame); err != nil {
		_ = conn.Close(websocket.StatusInternalError, "handshake write failed")
		cancel()
		return nil, model.WrapError(model.KindNetwork, err)
	}
	acked := false
	for !acked {
		_, data, err := conn.Read(hsCtx)
		if err != nil {
			cancel()
			return nil, model.WrapError(model.KindNetwork, fmt.Errorf("graphql handshake: %w", err))
		}
		var frame struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(data, &frame); err != nil {
			continue // 非法帧忽略
		}
		switch frame.Type {
		case "connection_ack":
			acked = true
		case "connection_error":
			_ = conn.Close(websocket.StatusNormalClosure, "connection_error")
			cancel()
			return nil, model.NewError(model.KindNetwork,
				"graphql connection rejected: "+string(frame.Payload))
		}
	}

	go s.readLoop(ctx)
	return s, nil
}

// readLoop 解包入站帧：next → in/data；error → in/error；complete → 系统关闭通知
func (s *graphqlWsSession) readLoop(ctx context.Context) {
	defer func() {
		_ = s.conn.Close(websocket.StatusNormalClosure, "")
	}()
	for {
		_, data, err := s.conn.Read(ctx)
		if err != nil {
			kind, detail := "close", err.Error()
			if status := websocket.CloseStatus(err); status != -1 {
				detail = status.String()
			}
			s.emit(InboundMsg{
				SessionId: s.id, Protocol: "graphql-ws", Direction: "system",
				Kind: kind, Data: detail, Ts: time.Now().UnixMilli(),
			})
			return
		}
		var frame struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(data, &frame); err != nil {
			continue
		}
		switch frame.Type {
		case "next":
			s.emit(InboundMsg{
				SessionId: s.id, Protocol: "graphql-ws", Direction: "in",
				Kind: "text", Data: string(frame.Payload), Ts: time.Now().UnixMilli(),
			})
		case "error":
			s.emit(InboundMsg{
				SessionId: s.id, Protocol: "graphql-ws", Direction: "in",
				Kind: "error", Data: string(frame.Payload), Ts: time.Now().UnixMilli(),
			})
		case "complete":
			s.emit(InboundMsg{
				SessionId: s.id, Protocol: "graphql-ws", Direction: "system",
				Kind: "close", Data: "server completed the subscription", Ts: time.Now().UnixMilli(),
			})
			return
		}
	}
}

func (s *graphqlWsSession) writeFrame(ctx context.Context, frame []byte) error {
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s.conn.Write(wctx, websocket.MessageText, frame)
}

// Send data 为订阅 payload：{query, variables?, operationName?}，包装为 subscribe 帧
func (s *graphqlWsSession) Send(data string) error {
	var payload json.RawMessage
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		return model.NewError(model.KindValidation, "graphql subscription payload must be JSON: "+err.Error())
	}
	frame, _ := json.Marshal(map[string]any{
		"type":    "subscribe",
		"id":      "1", // 一个会话一个订阅
		"payload": payload,
	})
	if err := s.writeFrame(context.Background(), frame); err != nil {
		return model.WrapError(model.KindNetwork, err)
	}
	s.emit(InboundMsg{
		SessionId: s.id, Protocol: "graphql-ws", Direction: "out",
		Kind: "text", Data: data, Ts: time.Now().UnixMilli(),
	})
	return nil
}

func (s *graphqlWsSession) Close() error {
	s.cancel()
	// 服务器 complete 后连接已自关：这里尽力关闭，已关不算失败
	_ = s.conn.Close(websocket.StatusNormalClosure, "client closed")
	return nil
}

func init() { registerOpener("graphql-ws", openGraphQLWs) }
