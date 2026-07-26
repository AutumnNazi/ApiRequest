package protocol

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// collector 线程安全地收集入站消息
type collector struct {
	mu   sync.Mutex
	msgs []InboundMsg
}

func (c *collector) emit(m InboundMsg) {
	c.mu.Lock()
	c.msgs = append(c.msgs, m)
	c.mu.Unlock()
}

func (c *collector) waitFor(t *testing.T, pred func([]InboundMsg) bool) []InboundMsg {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		snapshot := append([]InboundMsg(nil), c.msgs...)
		c.mu.Unlock()
		if pred(snapshot) {
			return snapshot
		}
		time.Sleep(20 * time.Millisecond)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	t.Fatalf("timeout waiting for messages, got: %+v", c.msgs)
	return nil
}

func TestWebSocketEcho(t *testing.T) {
	// echo 服务器
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx := r.Context()
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			conn.Write(ctx, typ, data)
		}
	}))
	defer srv.Close()

	m := NewManager()
	c := &collector{}
	wsUrl := "ws" + strings.TrimPrefix(srv.URL, "http")
	if err := m.Open("s1", SessionConfig{Protocol: "websocket", Url: wsUrl}, c.emit); err != nil {
		t.Fatalf("open: %v", err)
	}
	defer m.CloseAll()

	// open 事件
	c.waitFor(t, func(msgs []InboundMsg) bool {
		return len(msgs) >= 1 && msgs[0].Kind == "open"
	})

	if err := m.Send("s1", "hello ws"); err != nil {
		t.Fatalf("send: %v", err)
	}
	// out + 回显 in
	msgs := c.waitFor(t, func(msgs []InboundMsg) bool {
		hasOut, hasIn := false, false
		for _, m := range msgs {
			if m.Direction == "out" && m.Data == "hello ws" {
				hasOut = true
			}
			if m.Direction == "in" && m.Data == "hello ws" {
				hasIn = true
			}
		}
		return hasOut && hasIn
	})
	_ = msgs

	if err := m.Close("s1"); err != nil {
		t.Errorf("close: %v", err)
	}
	// 再发送应报会话不存在
	if err := m.Send("s1", "x"); err == nil {
		t.Error("send after close should error")
	}
}

func TestSSEStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		w.Write([]byte("event: greeting\ndata: hi\n\n"))
		flusher.Flush()
		w.Write([]byte("data: line1\ndata: line2\n\n"))
		flusher.Flush()
	}))
	defer srv.Close()

	m := NewManager()
	c := &collector{}
	if err := m.Open("s2", SessionConfig{Protocol: "sse", Url: srv.URL}, c.emit); err != nil {
		t.Fatalf("open: %v", err)
	}
	defer m.CloseAll()

	msgs := c.waitFor(t, func(msgs []InboundMsg) bool {
		events := 0
		for _, m := range msgs {
			if m.Kind == "event" {
				events++
			}
		}
		return events >= 2
	})

	var events []InboundMsg
	for _, m := range msgs {
		if m.Kind == "event" {
			events = append(events, m)
		}
	}
	if events[0].Event != "greeting" || events[0].Data != "hi" {
		t.Errorf("event 1 = %+v", events[0])
	}
	if events[1].Data != "line1\nline2" {
		t.Errorf("multi-line data = %q", events[1].Data)
	}

	// SSE 不支持发送
	if err := m.Send("s2", "x"); err == nil {
		t.Error("SSE send should error")
	}
}

func TestUnsupportedProtocol(t *testing.T) {
	m := NewManager()
	if err := m.Open("s3", SessionConfig{Protocol: "grpc", Url: "x"}, func(InboundMsg) {}); err == nil {
		t.Error("grpc should be unsupported for now")
	}
}
