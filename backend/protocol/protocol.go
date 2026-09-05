// Package protocol 实现多协议适配器（docs/protocols.md）。
// 每种协议实现 Session；生命周期由绑定方法打开/关闭，入站数据经回调推事件。
package protocol

import (
	"net/http"
	"sync"

	"apirequest/backend/model"
)

// SessionConfig 打开会话的配置
type SessionConfig struct {
	Protocol string     `json:"protocol"` // websocket | sse
	Url      string     `json:"url"`
	Headers  []model.KV `json:"headers,omitempty"`
}

// InboundMsg 入站消息（经事件推前端）
type InboundMsg struct {
	SessionId string `json:"sessionId"`
	Protocol  string `json:"protocol"`
	Direction string `json:"direction"` // in | out | system
	Kind      string `json:"kind"`      // text | binary | open | close | error | event
	Data      string `json:"data"`
	Event     string `json:"event,omitempty"`   // SSE 的 event 字段
	EventId   string `json:"eventId,omitempty"` // SSE id，用于 Last-Event-ID 续订
	Ts        int64  `json:"ts"`
}

// EmitFunc 入站回调（binding 层转 Wails 事件）
type EmitFunc func(msg InboundMsg)

// Session 一个活动会话
type Session interface {
	Send(data string) error
	Close() error
}

// opener 各协议的打开函数
type opener func(id string, cfg SessionConfig, emit EmitFunc, client *http.Client) (Session, error)

var openers = map[string]opener{}

// registerOpener 注册协议（init 时调用）
func registerOpener(protocol string, fn opener) { openers[protocol] = fn }

// Manager 管理全部活动会话
type Manager struct {
	mu       sync.Mutex
	sessions map[string]Session
	opening  map[string]struct{}
	// closing 记录"dial 尚未完成时就有人调 Close"的 id：Open 的注册阶段
	// 检查此表，避免 dial 完成后把一个前端已关闭的会话注册进 sessions（泄漏）
	closing map[string]struct{}
	client  *http.Client
}

// NewManager 构造
func NewManager(clients ...*http.Client) *Manager {
	client := http.DefaultClient
	if len(clients) > 0 && clients[0] != nil {
		client = clients[0]
	}
	return &Manager{
		sessions: map[string]Session{},
		opening:  map[string]struct{}{},
		closing:  map[string]struct{}{},
		client:   client,
	}
}

// Open 打开会话。sessionId 由前端生成。
func (m *Manager) Open(sessionId string, cfg SessionConfig, emit EmitFunc) error {
	fn, ok := openers[cfg.Protocol]
	if !ok {
		return model.NewError(model.KindValidation, "unsupported protocol: "+cfg.Protocol)
	}
	m.mu.Lock()
	_, active := m.sessions[sessionId]
	_, opening := m.opening[sessionId]
	if active || opening {
		m.mu.Unlock()
		return model.NewError(model.KindValidation, "session already exists: "+sessionId)
	}
	m.opening[sessionId] = struct{}{}
	m.mu.Unlock()

	s, err := fn(sessionId, cfg, emit, m.client)
	m.mu.Lock()
	delete(m.opening, sessionId)
	if err != nil {
		// dial 失败同样要清理 closing 标记：Close 在 dial 窗口内到达后 dial
		// 又失败时，会话从未存在，标记不清会永久残留（复用同 id 的下次
		// Open 会被误判为"打开期间已关闭"）
		delete(m.closing, sessionId)
		m.mu.Unlock()
		return err
	}
	// dial 期间有人调过 Close（如前端面板关闭）：不注册，立即关闭并清理标记
	if _, closePending := m.closing[sessionId]; closePending {
		delete(m.closing, sessionId)
		m.mu.Unlock()
		s.Close()
		return model.NewError(model.KindNetwork, "session closed while opening")
	}
	m.sessions[sessionId] = s
	m.mu.Unlock()
	return nil
}

// Send 向会话发送数据
func (m *Manager) Send(sessionId, data string) error {
	m.mu.Lock()
	s, ok := m.sessions[sessionId]
	m.mu.Unlock()
	if !ok {
		return model.NewError(model.KindValidation, "session not found: "+sessionId)
	}
	return s.Send(data)
}

// Close 关闭会话（未知 id 为 no-op）。
// dial 尚未完成的 id 记入 closing 表（Open 注册时检查）——直接返回 nil，
// 否则 Close 在 opening 窗口内到达会被当 no-op 丢弃，dial 完成后无人再关
func (m *Manager) Close(sessionId string) error {
	m.mu.Lock()
	s, ok := m.sessions[sessionId]
	if ok {
		delete(m.sessions, sessionId)
	} else if _, opening := m.opening[sessionId]; opening {
		m.closing[sessionId] = struct{}{}
	}
	m.mu.Unlock()
	if ok {
		return s.Close()
	}
	return nil
}

// Remove 会话自然结束时从表中移除（适配器内部用）
func (m *Manager) Remove(sessionId string) {
	m.mu.Lock()
	delete(m.sessions, sessionId)
	m.mu.Unlock()
}

// CloseAll 应用退出时清理
func (m *Manager) CloseAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.Close(id)
	}
}
