package protocol

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// 进程内 graphql-transport-ws 服务器：完成握手 → 收 subscribe → 推一条 next → complete
func startGraphqlWsServer(t *testing.T, nextPayload string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			Subprotocols: []string{"graphql-transport-ws"},
		})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		ctx := r.Context()

		// 握手
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var initFrame struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(data, &initFrame) != nil || initFrame.Type != "connection_init" {
			return
		}
		writeJSON(t, conn, map[string]string{"type": "connection_ack"})

		// subscribe
		_, data, err = conn.Read(ctx)
		if err != nil {
			return
		}
		var sub struct {
			Type    string `json:"type"`
			ID      string `json:"id"`
			Payload struct {
				Query string `json:"query"`
			} `json:"payload"`
		}
		if json.Unmarshal(data, &sub) != nil || sub.Type != "subscribe" {
			return
		}
		if !strings.Contains(sub.Payload.Query, "countUp") {
			writeJSON(t, conn, map[string]any{"type": "error", "id": sub.ID, "payload": map[string]string{"message": "bad query"}})
			return
		}
		writeJSON(t, conn, map[string]any{"type": "next", "id": sub.ID, "payload": map[string]any{"data": json.RawMessage(nextPayload)}})
		writeJSON(t, conn, map[string]any{"type": "complete", "id": sub.ID})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeJSON(t *testing.T, conn *websocket.Conn, v any) {
	t.Helper()
	data, _ := json.Marshal(v)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Errorf("server write: %v", err)
	}
}

func TestGraphqlWsSessionRoundTrip(t *testing.T) {
	srv := startGraphqlWsServer(t, `{"countUp":42}`)
	events := make(chan InboundMsg, 32)
	emit := func(msg InboundMsg) { events <- msg }

	m := NewManager()
	sessionId := "gql-1"
	err := m.Open(sessionId, SessionConfig{
		Protocol: "graphql-ws",
		Url:      "ws://" + strings.TrimPrefix(srv.URL, "http://"),
	}, emit)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer m.Close(sessionId)

	if err := m.Send(sessionId, `{"query":"subscription { countUp }"}`); err != nil {
		t.Fatalf("send subscribe: %v", err)
	}

	// 依次收到：open（握手前）、in/data（next 解包）、system/close（complete）
	var gotNext, gotComplete bool
	deadline := time.After(5 * time.Second)
	for !(gotNext && gotComplete) {
		select {
		case msg := <-events:
			switch {
			case msg.Direction == "in" && msg.Kind == "text" && strings.Contains(msg.Data, "countUp"):
				gotNext = true
			case msg.Direction == "system" && msg.Kind == "close" && strings.Contains(msg.Data, "completed"):
				gotComplete = true
			}
		case <-deadline:
			t.Fatalf("timeout: next=%v complete=%v", gotNext, gotComplete)
		}
	}

	// 服务器完成订阅后会话应自动可关（Close 不报错即可）
	if err := m.Close(sessionId); err != nil {
		t.Fatalf("close after complete: %v", err)
	}
}

func TestGraphqlWsConnectionRejected(t *testing.T) {
	// 服务器拒绝连接：返回 connection_error → Open 应失败
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			Subprotocols: []string{"graphql-transport-ws"},
		})
		if err != nil {
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "")
		_, data, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		var initFrame struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(data, &initFrame) != nil || initFrame.Type != "connection_init" {
			return
		}
		writeJSON(t, conn, map[string]any{"type": "connection_error", "payload": map[string]string{"message": "unauthorized"}})
	}))
	defer srv.Close()

	err := NewManager().Open("gql-reject", SessionConfig{
		Protocol: "graphql-ws",
		Url:      "ws://" + strings.TrimPrefix(srv.URL, "http://"),
	}, func(msg InboundMsg) {})
	if err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("err = %v, want connection rejected with unauthorized", err)
	}
}
