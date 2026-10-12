package protocol

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCloseAllPreventsOpeningSessionFromRegistering(t *testing.T) {
	const name = "shutdown-open"
	started, release := make(chan struct{}), make(chan struct{})
	session := &fakeSession{}
	openers[name] = func(context.Context, string, SessionConfig, EmitFunc, *http.Client) (Session, error) {
		close(started)
		<-release
		return session, nil
	}
	defer delete(openers, name)
	manager := NewManager()
	result := make(chan error, 1)
	go func() { result <- manager.Open("pending", SessionConfig{Protocol: name}, func(InboundMsg) {}) }()
	<-started
	manager.CloseAll()
	close(release)
	if err := <-result; err == nil {
		t.Error("opening session registered after shutdown")
	}
	if err := manager.Send("pending", "test"); err == nil {
		t.Error("closed manager still routes messages")
	}
	if got := atomic.LoadInt32(&session.closed); got != 1 {
		t.Errorf("session close count = %d, want 1", got)
	}
}

func TestCloseAllCancelsPendingHandshake(t *testing.T) {
	for _, protocolName := range []string{"sse", "websocket", "graphql-ws"} {
		t.Run(protocolName, func(t *testing.T) {
			started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				select {
				case <-r.Context().Done():
					close(cancelled)
				case <-release:
				}
			}))
			defer server.Close()
			manager := NewManager(server.Client())
			url := server.URL
			if protocolName != "sse" {
				url = "ws" + strings.TrimPrefix(url, "http")
			}
			done := make(chan error, 1)
			go func() {
				done <- manager.Open("pending", SessionConfig{Protocol: protocolName, Url: url}, func(InboundMsg) {})
			}()
			defer func() { close(release); <-done; manager.CloseAll() }()
			<-started
			manager.CloseAll()
			select {
			case <-cancelled:
			case <-time.After(3 * time.Second):
				t.Fatal("shutdown did not cancel the pending HTTP request")
			}
		})
	}
}

func TestClosedManagerRejectsNewConnections(t *testing.T) {
	const name = "closed-manager"
	opened := false
	openers[name] = func(context.Context, string, SessionConfig, EmitFunc, *http.Client) (Session, error) {
		opened = true
		return &fakeSession{}, nil
	}
	defer delete(openers, name)
	manager := NewManager()
	manager.CloseAll()
	manager.CloseAll()
	if err := manager.Open("late", SessionConfig{Protocol: name}, nil); err == nil {
		t.Fatal("closed manager accepted a new connection")
	}
	if opened {
		t.Fatal("closed manager called the opener")
	}
}

func TestLateTerminalEventCannotRemoveReplacementSession(t *testing.T) {
	const name = "reused-id"
	var emitters []EmitFunc
	openers[name] = func(_ context.Context, _ string, _ SessionConfig, emit EmitFunc, _ *http.Client) (Session, error) {
		emitters = append(emitters, emit)
		return &fakeSession{}, nil
	}
	defer delete(openers, name)
	manager := NewManager()
	defer manager.CloseAll()
	var delivered []string
	receive := func(msg InboundMsg) { delivered = append(delivered, msg.Data) }
	if err := manager.Open("same", SessionConfig{Protocol: name}, receive); err != nil {
		t.Fatal(err)
	}
	if err := manager.Close("same"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Open("same", SessionConfig{Protocol: name}, receive); err != nil {
		t.Fatal(err)
	}
	emitters[0](InboundMsg{SessionId: "same", Kind: "close", Data: "old-close"})
	if err := manager.Send("same", "still-alive"); err != nil {
		t.Fatal(err)
	}
	emitters[1](InboundMsg{SessionId: "same", Kind: "text", Data: "current"})
	if len(delivered) != 1 || delivered[0] != "current" {
		t.Fatalf("events=%v", delivered)
	}
}

func TestTerminalEventDuringOpenDoesNotLeaveGhostSession(t *testing.T) {
	const name = "early-terminal"
	session := &fakeSession{}
	openers[name] = func(_ context.Context, id string, _ SessionConfig, emit EmitFunc, _ *http.Client) (Session, error) {
		emit(InboundMsg{SessionId: id, Kind: "close"})
		return session, nil
	}
	defer delete(openers, name)
	manager := NewManager()
	if err := manager.Open("ended", SessionConfig{Protocol: name}, nil); err == nil {
		t.Fatal("ended session was activated")
	}
	if err := manager.Send("ended", "message"); err == nil {
		t.Fatal("ghost session remained registered")
	}
	if atomic.LoadInt32(&session.closed) != 1 {
		t.Fatal("returned session was not released")
	}
}

// fakeSession 记录 Close 被调用的次数
type fakeSession struct {
	closed int32
}

func (f *fakeSession) Send(data string) error { return nil }
func (f *fakeSession) Close() error {
	atomic.AddInt32(&f.closed, 1)
	return nil
}

// 回归：前端在 connect 进行中关闭面板（Close 到达时 session 尚在 opening 表中）
// 不得泄漏会话。原缺陷：Manager.Close 只查 sessions，对 opening 中的 id 是 no-op；
// dial 完成后 session 被注册进 sessions，但前端已卸载，连接活到应用退出。
func TestCloseDuringOpenDoesNotLeakSession(t *testing.T) {
	srv := &fakeSession{}
	releaseDial := make(chan struct{})
	dialStarted := make(chan struct{})

	opener := func(ctx context.Context, id string, cfg SessionConfig, emit EmitFunc, client *http.Client) (Session, error) {
		close(dialStarted)
		<-releaseDial // 模拟 15s dial 窗口，测试里由我们控制
		return srv, nil
	}
	openers["test-close-during-open"] = opener
	defer delete(openers, "test-close-during-open")

	m := NewManager(&http.Client{})
	openDone := make(chan error, 1)
	go func() {
		// 该场景下 Open 返回 "session closed while opening" 是预期行为
		// （dial 期间前端已关闭），不算错误
		openDone <- m.Open("ws-1", SessionConfig{Protocol: "test-close-during-open", Url: "http://x"}, func(InboundMsg) {})
	}()

	<-dialStarted
	m.Close("ws-1")
	close(releaseDial)
	if err := <-openDone; err == nil {
		t.Fatal("closed opening session was accepted")
	}

	if got := atomic.LoadInt32(&srv.closed); got != 1 {
		t.Errorf("Close calls = %d, want 1 (session leaked if 0)", got)
	}
	if _, leaked := m.sessions["ws-1"]; leaked {
		t.Error("session remains registered after close-during-open")
	}
}

// Close 在 Open 失败之后到达：不误报
func TestCloseAfterFailedOpenIsNoOp(t *testing.T) {
	opener := func(ctx context.Context, id string, cfg SessionConfig, emit EmitFunc, client *http.Client) (Session, error) {
		return nil, errors.New("dial failed")
	}
	openers["test-open-fail"] = opener
	defer delete(openers, "test-open-fail")

	m := NewManager(&http.Client{})
	if err := m.Open("ws-2", SessionConfig{Protocol: "test-open-fail"}, func(InboundMsg) {}); err == nil {
		t.Fatal("expected open failure")
	}
	if err := m.Close("ws-2"); err != nil {
		t.Fatalf("Close after failed open: %v", err)
	}
}

// Close 后拨号失败，连接任务必须释放，避免阻止复用同一 ID。
func TestOperationClearedWhenDialFails(t *testing.T) {
	releaseDial := make(chan struct{})
	dialStarted := make(chan struct{})
	opener := func(ctx context.Context, id string, cfg SessionConfig, emit EmitFunc, client *http.Client) (Session, error) {
		close(dialStarted)
		<-releaseDial
		return nil, errors.New("dial failed")
	}
	openers["test-close-dial-fail"] = opener
	defer delete(openers, "test-close-dial-fail")

	m := NewManager(&http.Client{})
	openErr := make(chan error, 1)
	go func() {
		openErr <- m.Open("ws-3", SessionConfig{Protocol: "test-close-dial-fail"}, func(InboundMsg) {})
	}()

	<-dialStarted   // dial 进行中
	m.Close("ws-3") // 前端卸载：此刻仍在拨号。
	close(releaseDial)
	if err := <-openErr; err == nil {
		t.Fatal("expected open failure after released dial")
	}

	m.mu.Lock()
	_, leaked := m.operations["ws-3"]
	m.mu.Unlock()
	if leaked {
		t.Error("operation leaked after failed dial")
	}
}
