package protocol

import (
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

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

	opener := func(id string, cfg SessionConfig, emit EmitFunc, client *http.Client) (Session, error) {
		close(dialStarted)
		<-releaseDial // 模拟 15s dial 窗口，测试里由我们控制
		return srv, nil
	}
	openers["test-close-during-open"] = opener
	defer delete(openers, "test-close-during-open")

	m := NewManager(&http.Client{})
	go func() {
		// 该场景下 Open 返回 "session closed while opening" 是预期行为
		// （dial 期间前端已关闭），不算错误
		_ = m.Open("ws-1", SessionConfig{Protocol: "test-close-during-open", Url: "http://x"}, func(InboundMsg) {})
	}()

	<-dialStarted                     // Open 已进入 dial（opening 表中）
	m.Close("ws-1")                   // 前端卸载时关闭：此刻 sessions 里还没有它
	close(releaseDial)                // dial 完成，session 即将注册
	time.Sleep(50 * time.Millisecond) // 等 Open 的注册完成

	if got := atomic.LoadInt32(&srv.closed); got != 1 {
		t.Errorf("Close calls = %d, want 1 (session leaked if 0)", got)
	}
	if _, leaked := m.sessions["ws-1"]; leaked {
		t.Error("session remains registered after close-during-open")
	}
}

// Close 在 Open 失败之后到达：不误报
func TestCloseAfterFailedOpenIsNoOp(t *testing.T) {
	opener := func(id string, cfg SessionConfig, emit EmitFunc, client *http.Client) (Session, error) {
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

// 回归：Close 到达时 dial 还在进行，随后 dial 失败——closing 标记必须清理，
// 否则该 id 的标记永久残留（map 缓慢增长，且若前端重试复用同 id 会被误判为"已关闭"）
func TestClosingMarkerClearedWhenDialFails(t *testing.T) {
	releaseDial := make(chan struct{})
	dialStarted := make(chan struct{})
	opener := func(id string, cfg SessionConfig, emit EmitFunc, client *http.Client) (Session, error) {
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
	m.Close("ws-3") // 前端卸载：此刻还在 opening 表中，标记进 closing
	close(releaseDial)
	if err := <-openErr; err == nil {
		t.Fatal("expected open failure after released dial")
	}

	m.mu.Lock()
	_, leaked := m.closing["ws-3"]
	m.mu.Unlock()
	if leaked {
		t.Error("closing marker leaked after failed dial")
	}
}
