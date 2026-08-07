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

type protocolRoundTripper func(*http.Request) (*http.Response, error)

func (fn protocolRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
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

func TestSSEUsesInjectedHTTPClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Network-Policy") != "shared" {
			http.Error(w, "missing shared client", http.StatusTeapot)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: ready\n\n"))
	}))
	defer server.Close()
	client := &http.Client{Transport: protocolRoundTripper(func(request *http.Request) (*http.Response, error) {
		request.Header.Set("X-Network-Policy", "shared")
		return http.DefaultTransport.RoundTrip(request)
	})}
	manager := NewManager(client)
	messages := &collector{}
	if err := manager.Open("shared-client", SessionConfig{Protocol: "sse", Url: server.URL}, messages.emit); err != nil {
		t.Fatal(err)
	}
	defer manager.CloseAll()
	messages.waitFor(t, func(items []InboundMsg) bool {
		for _, item := range items {
			if item.Kind == "event" && item.Data == "ready" {
				return true
			}
		}
		return false
	})
}

func TestSSEReconnectUsesLastEventIDAndCloseStopsRetries(t *testing.T) {
	var mu sync.Mutex
	connections := 0
	lastEventHeaders := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		connections++
		connection := connections
		lastEventHeaders = append(lastEventHeaders, r.Header.Get("Last-Event-ID"))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		if connection == 1 {
			_, _ = w.Write([]byte("id: 42\nretry: 100\nevent: update\ndata: first\n\n"))
			flusher.Flush()
			return
		}
		_, _ = w.Write([]byte("id: 43\nevent: update\ndata: second\n\n"))
		flusher.Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	manager := NewManager()
	collector := &collector{}
	if err := manager.Open("sse-reconnect", SessionConfig{Protocol: "sse", Url: srv.URL}, collector.emit); err != nil {
		t.Fatal(err)
	}
	messages := collector.waitFor(t, func(messages []InboundMsg) bool {
		events := 0
		reconnected := false
		for _, message := range messages {
			if message.Kind == "event" {
				events++
			}
			if message.Kind == "reconnect" && message.Data == "reconnected" {
				reconnected = true
			}
		}
		return events >= 2 && reconnected
	})
	var eventIDs []string
	for _, message := range messages {
		if message.Kind == "event" {
			eventIDs = append(eventIDs, message.EventId)
		}
	}
	if len(eventIDs) < 2 || eventIDs[0] != "42" || eventIDs[1] != "43" {
		t.Fatalf("event ids = %v", eventIDs)
	}
	mu.Lock()
	headers := append([]string(nil), lastEventHeaders...)
	mu.Unlock()
	if len(headers) < 2 || headers[0] != "" || headers[1] != "42" {
		t.Fatalf("Last-Event-ID headers = %v", headers)
	}
	if err := manager.Close("sse-reconnect"); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	closedAt := connections
	mu.Unlock()
	time.Sleep(250 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if connections != closedAt {
		t.Fatalf("connections after close = %d, want %d", connections, closedAt)
	}
}

func TestUnsupportedProtocol(t *testing.T) {
	m := NewManager()
	if err := m.Open("s3", SessionConfig{Protocol: "grpc", Url: "x"}, func(InboundMsg) {}); err == nil {
		t.Error("grpc should be unsupported for now")
	}
}

type duplicateTestSession struct{ closed bool }

func (*duplicateTestSession) Send(string) error { return nil }
func (s *duplicateTestSession) Close() error {
	s.closed = true
	return nil
}

func TestManagerRejectsDuplicateSessionIDBeforeOpening(t *testing.T) {
	const protocolName = "duplicate-test"
	opened := 0
	first := &duplicateTestSession{}
	openers[protocolName] = func(string, SessionConfig, EmitFunc, *http.Client) (Session, error) {
		opened++
		if opened == 1 {
			return first, nil
		}
		return &duplicateTestSession{}, nil
	}
	defer delete(openers, protocolName)

	manager := NewManager()
	config := SessionConfig{Protocol: protocolName, Url: "unused"}
	if err := manager.Open("same", config, func(InboundMsg) {}); err != nil {
		t.Fatal(err)
	}
	if err := manager.Open("same", config, func(InboundMsg) {}); err == nil {
		t.Fatal("duplicate session id was accepted")
	}
	if opened != 1 || first.closed {
		t.Fatalf("opened = %d, first closed = %v", opened, first.closed)
	}
	manager.CloseAll()
}
