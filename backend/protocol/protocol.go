// Package protocol 实现多协议适配器（docs/protocols.md）。
// 每种协议实现 Session；生命周期由绑定方法打开/关闭，入站数据经回调推事件。
package protocol

import (
	"context"
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
type opener func(ctx context.Context, id string, cfg SessionConfig, emit EmitFunc, client *http.Client) (Session, error)

var openers = map[string]opener{}

// registerOpener 注册协议（init 时调用）
func registerOpener(protocol string, fn opener) { openers[protocol] = fn }

type sessionOperation struct {
	cancel context.CancelFunc
	ended  bool // 在管理器锁内记录终态，旧会话不能影响同 ID 的新任务。
}

// Manager 管理连接中任务与活动会话的统一生命周期。
type Manager struct {
	mu         sync.Mutex
	sessions   map[string]Session
	operations map[string]*sessionOperation
	closed     bool
	client     *http.Client
}

// NewManager 构造
func NewManager(clients ...*http.Client) *Manager {
	client := http.DefaultClient
	if len(clients) > 0 && clients[0] != nil {
		client = clients[0]
	}
	return &Manager{
		sessions:   map[string]Session{},
		operations: map[string]*sessionOperation{},
		client:     client,
	}
}

// Open 打开会话。sessionId 由前端生成。
func (m *Manager) Open(sessionId string, cfg SessionConfig, emit EmitFunc) error {
	fn, ok := openers[cfg.Protocol]
	if !ok {
		return model.NewError(model.KindValidation, "unsupported protocol: "+cfg.Protocol)
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return model.NewError(model.KindNetwork, "session manager is closed")
	}
	if _, exists := m.operations[sessionId]; exists {
		m.mu.Unlock()
		return model.NewError(model.KindValidation, "session already exists: "+sessionId)
	}
	ctx, cancel := context.WithCancel(context.Background())
	operation := &sessionOperation{cancel: cancel}
	m.operations[sessionId] = operation
	m.mu.Unlock()

	// 终态事件由管理器处理，并用任务身份过滤旧连接的晚到事件。
	emitCurrent := func(msg InboundMsg) {
		terminal := msg.Kind == "close" || msg.Kind == "error"
		m.mu.Lock()
		current := m.operations[sessionId] == operation
		// 显式 Close 后仍允许一次关闭通知，但不得串到复用 ID 的新连接。
		deliver := current || (terminal && m.operations[sessionId] == nil && !operation.ended && !m.closed)
		if terminal {
			operation.ended = true
			if current {
				delete(m.operations, sessionId)
				delete(m.sessions, sessionId)
			}
		}
		m.mu.Unlock()
		if terminal {
			cancel()
		}
		if deliver && emit != nil {
			emit(msg)
		}
	}
	s, err := fn(ctx, sessionId, cfg, emitCurrent, m.client)
	m.mu.Lock()
	current := m.operations[sessionId] == operation
	if err != nil {
		if current {
			delete(m.operations, sessionId)
		}
		m.mu.Unlock()
		cancel()
		return err
	}
	// 即使 Adapter 在取消后仍返回成功，也不允许迟到的连接重新注册。
	if !current {
		m.mu.Unlock()
		cancel()
		_ = s.Close()
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

// Close 关闭会话并取消仍在拨号或握手的任务（未知 id 为 no-op）。
func (m *Manager) Close(sessionId string) error {
	m.mu.Lock()
	s, ok := m.sessions[sessionId]
	operation := m.operations[sessionId]
	delete(m.sessions, sessionId)
	delete(m.operations, sessionId)
	m.mu.Unlock()
	if operation != nil {
		operation.cancel()
	}
	if ok {
		return s.Close()
	}
	return nil
}

// CloseAll 停止接收新任务，取消全部连接中任务并关闭活动会话。
func (m *Manager) CloseAll() {
	m.mu.Lock()
	m.closed = true
	operations, sessions := m.operations, m.sessions
	m.operations = map[string]*sessionOperation{}
	m.sessions = map[string]Session{}
	m.mu.Unlock()
	for _, operation := range operations {
		operation.cancel()
	}
	for _, session := range sessions {
		_ = session.Close()
	}
}
