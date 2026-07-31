package grpcclient

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"apirequest/backend/model"
)

// StreamMessage 流式会话经回调推送的单条入站消息（封装 JSON 后给前端）
type StreamMessage struct {
	StreamId string `json:"streamId"`
	Kind     string `json:"kind"` // message | error | done
	Data     string `json:"data"` // kind=message:单条 JSON 响应；kind=error:错误文本
	Ts       int64  `json:"ts"`   // Unix 毫秒
}

// StreamSession 一个运行中的 gRPC 流会话
type StreamSession struct {
	ID         string
	Cancel     context.CancelFunc
	Desc       *grpc.StreamDesc
	Stream     grpc.ClientStream
	InputMD    protoreflect.MessageDescriptor
	OutputMD   protoreflect.MessageDescriptor
	mu         sync.Mutex
	closed     bool
	sendClosed bool
}

// SendToStream 向流发送一条消息；server-stream 也需要用它发送唯一的初始请求。
func (s *StreamSession) SendToStream(jsonPayload string) error {
	if s == nil || s.Stream == nil || s.InputMD == nil {
		return model.NewError(model.KindValidation, "stream not open")
	}
	msg := dynamicpb.NewMessage(s.InputMD)
	if strings.TrimSpace(jsonPayload) != "" {
		if err := protojson.Unmarshal([]byte(jsonPayload), msg); err != nil {
			return model.NewError(model.KindValidation, "request JSON does not match input message: "+err.Error())
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return model.NewError(model.KindValidation, "stream already closed")
	}
	if s.sendClosed {
		return model.NewError(model.KindValidation, "stream send side already closed")
	}
	if err := s.Stream.SendMsg(msg); err != nil {
		return model.WrapError(model.KindNetwork, err)
	}
	return nil
}

// CloseStream 主动关闭流（发送 Done；服务端 RecvEOF 后会结束）
func (s *StreamSession) CloseStream() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	sendWasClosed := s.sendClosed
	s.closed = true
	s.sendClosed = true
	if s.Stream != nil && !sendWasClosed {
		_ = s.Stream.CloseSend()
	}
	cancel := s.Cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (s *StreamSession) CloseSend() error {
	if s == nil || s.Stream == nil {
		return model.NewError(model.KindValidation, "stream not open")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.sendClosed {
		return nil
	}
	if err := s.Stream.CloseSend(); err != nil {
		return model.WrapError(model.KindNetwork, err)
	}
	s.sendClosed = true
	return nil
}

func (s *StreamSession) markClosed() {
	s.mu.Lock()
	s.closed = true
	s.sendClosed = true
	s.mu.Unlock()
}

type streamOpening struct {
	cancel context.CancelFunc
}

// streamManager 简易会话表（与 protocol.Manager 类似，但 gRPC 专用）
type streamManager struct {
	mu       sync.Mutex
	sessions map[string]*StreamSession
	opening  map[string]*streamOpening
}

func newStreamManager() *streamManager {
	return &streamManager{
		sessions: map[string]*StreamSession{},
		opening:  map[string]*streamOpening{},
	}
}

var defaultStreamMgr = newStreamManager()

func (m *streamManager) reserve(id string, cancel context.CancelFunc) (*streamOpening, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[id]; ok {
		return nil, model.NewError(model.KindValidation, "stream already exists: "+id)
	}
	if _, ok := m.opening[id]; ok {
		return nil, model.NewError(model.KindValidation, "stream already opening: "+id)
	}
	token := &streamOpening{cancel: cancel}
	m.opening[id] = token
	return token, nil
}

func (m *streamManager) activate(id string, token *streamOpening, sess *StreamSession) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.opening[id] != token {
		return false
	}
	delete(m.opening, id)
	m.sessions[id] = sess
	return true
}

func (m *streamManager) abort(id string, token *streamOpening) {
	m.mu.Lock()
	if m.opening[id] == token {
		delete(m.opening, id)
	}
	m.mu.Unlock()
}

func (m *streamManager) finish(id string, sess *StreamSession) {
	m.mu.Lock()
	if m.sessions[id] == sess {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
}

func (m *streamManager) get(id string) (*StreamSession, bool) {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	m.mu.Unlock()
	return sess, ok
}

func (m *streamManager) cancelOpening(id string) bool {
	m.mu.Lock()
	opening, ok := m.opening[id]
	if ok {
		delete(m.opening, id)
	}
	m.mu.Unlock()
	if ok {
		opening.cancel()
	}
	return ok
}

func (m *streamManager) closeAll() {
	m.mu.Lock()
	sessions := make([]*StreamSession, 0, len(m.sessions))
	for _, sess := range m.sessions {
		sessions = append(sessions, sess)
	}
	openings := make([]*streamOpening, 0, len(m.opening))
	for _, opening := range m.opening {
		openings = append(openings, opening)
	}
	m.sessions = map[string]*StreamSession{}
	m.opening = map[string]*streamOpening{}
	m.mu.Unlock()

	for _, opening := range openings {
		opening.cancel()
	}
	for _, sess := range sessions {
		_ = sess.CloseStream()
	}
}

// OpenStream 启动一个流式调用；onMsg 用于把入站消息回推前端（事件循环里 marshal 成 JSON）。
// 返回的 sessionId 由前端持有用于后续 send/close。
//
// 实现关键点：
//   - 用 conn.NewStream 创建 ClientStream；StreamDesc 标记 client/server-stream 双向
//   - 入站消息在一个独立 goroutine 内 Recv 循环，遇到 EOF 停止；错误推送给前端
//   - unary-style 不走此路径（仍由 Call 完成）
func OpenStream(cfg ConnectConfig, fullMethod, sessionId string, headers map[string]string, onMsg func(StreamMessage)) (*StreamSession, error) {
	if sessionId == "" {
		return nil, model.NewError(model.KindValidation, "sessionId is required")
	}
	if strings.TrimSpace(cfg.Target) == "" {
		return nil, model.NewError(model.KindValidation, "target is required")
	}
	trimmed := strings.TrimPrefix(fullMethod, "/")
	slash := strings.LastIndex(trimmed, "/")
	if !strings.HasPrefix(fullMethod, "/") || slash <= 0 || slash == len(trimmed)-1 {
		return nil, model.NewError(model.KindValidation, "method must be /package.Service/Method")
	}
	svcName, methodName := trimmed[:slash], trimmed[slash+1:]

	ctx, cancel := context.WithCancel(context.Background())
	opening, err := defaultStreamMgr.reserve(sessionId, cancel)
	if err != nil {
		cancel()
		return nil, err
	}
	activated := false
	defer func() {
		if !activated {
			defaultStreamMgr.abort(sessionId, opening)
			cancel()
		}
	}()

	conn, err := dial(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// reflection 发现应有界；真正 stream 使用无 deadline 的 ctx 保持长连接。
	reflectionCtx, reflectionCancel := context.WithTimeout(ctx, timeoutOf(cfg))
	files, _, err := fetchDescriptors(reflectionCtx, conn)
	reflectionCancel()
	if err != nil {
		conn.Close()
		return nil, err
	}
	desc, err := files.FindDescriptorByName(protoreflect.FullName(svcName))
	if err != nil {
		conn.Close()
		return nil, model.NewError(model.KindValidation, "service not found: "+svcName)
	}
	svc, ok := desc.(protoreflect.ServiceDescriptor)
	if !ok {
		conn.Close()
		return nil, model.NewError(model.KindValidation, "not a service: "+svcName)
	}
	m := svc.Methods().ByName(protoreflect.Name(methodName))
	if m == nil {
		conn.Close()
		return nil, model.NewError(model.KindValidation, "method not found: "+methodName)
	}
	if !m.IsStreamingClient() && !m.IsStreamingServer() {
		conn.Close()
		return nil, model.NewError(model.KindValidation, "method is not a streaming rpc; use unary call")
	}

	if len(headers) > 0 {
		pairs := make([]string, 0, len(headers)*2)
		for k, v := range headers {
			pairs = append(pairs, k, v)
		}
		ctx = metadata.AppendToOutgoingContext(ctx, pairs...)
	}

	streamDesc := &grpc.StreamDesc{
		StreamName:    string(m.Name()),
		ServerStreams: m.IsStreamingServer(),
		ClientStreams: m.IsStreamingClient(),
	}

	// 创建 ClientStream（conn.NewStream 是 *grpc.ClientConn 提供的公开方法）
	stream, err := conn.NewStream(ctx, streamDesc, fullMethod)
	if err != nil {
		conn.Close()
		return nil, model.WrapError(model.KindNetwork, err)
	}

	sess := &StreamSession{
		ID:       sessionId,
		Cancel:   cancel,
		Desc:     streamDesc,
		Stream:   stream,
		InputMD:  m.Input(),
		OutputMD: m.Output(),
	}

	if !defaultStreamMgr.activate(sessionId, opening, sess) {
		_ = stream.CloseSend()
		conn.Close()
		return nil, model.NewError(model.KindValidation, "stream opening canceled: "+sessionId)
	}
	activated = true

	// 接收循环（独立 goroutine）：仅 server-stream/bidi 时才循环；纯 client-stream 跳过
	go func() {
		defer func() {
			sess.markClosed()
			defaultStreamMgr.finish(sessionId, sess)
			cancel()
			conn.Close()
		}()
		marshaler := protojson.MarshalOptions{Multiline: true, Indent: "  "}
		// 即使 client-only stream 没有 server 响应，单次 Recv 也能拿到 trailer
		for {
			outMsg := dynamicpb.NewMessage(sess.OutputMD)
			err := stream.RecvMsg(outMsg)
			if err != nil {
				// io.EOF 表示完成
				if isEOF(err) {
					if onMsg != nil {
						onMsg(StreamMessage{StreamId: sessionId, Kind: "done", Ts: nowMillis()})
					}
					return
				}
				if onMsg != nil {
					onMsg(StreamMessage{StreamId: sessionId, Kind: "error", Data: err.Error(), Ts: nowMillis()})
				}
				return
			}
			data, mErr := marshaler.Marshal(outMsg)
			if mErr != nil {
				if onMsg != nil {
					onMsg(StreamMessage{StreamId: sessionId, Kind: "error", Data: "marshal: " + mErr.Error(), Ts: nowMillis()})
				}
				continue
			}
			if onMsg != nil {
				onMsg(StreamMessage{StreamId: sessionId, Kind: "message", Data: string(data), Ts: nowMillis()})
			}
			// 单条 server-stream（非 bidi）也走 Recv 循环，遇 EOF 退出
		}
	}()

	return sess, nil
}

// SendStream 向（已打开的）流发送一条消息
func SendStream(sessionId, jsonPayload string) error {
	sess, ok := defaultStreamMgr.get(sessionId)
	if !ok {
		return model.NewError(model.KindValidation, "stream not found: "+sessionId)
	}
	return sess.SendToStream(jsonPayload)
}

// CloseStream 主动关闭并销毁流会话（cancel + CloseSend）
func CloseStream(sessionId string) error {
	if defaultStreamMgr.cancelOpening(sessionId) {
		return nil
	}
	sess, ok := defaultStreamMgr.get(sessionId)
	if !ok {
		return nil // 已结束视为成功
	}
	return sess.CloseStream()
}

// CloseSendStream 仅向 client-stream 半关（CloseSend）保留 ctx 不 cancel，
// 让服务端继续往回推送消息直到 EOF；用于"client-stream-only 发完即关"场景。
func CloseSendStream(sessionId string) error {
	sess, ok := defaultStreamMgr.get(sessionId)
	if !ok {
		return nil
	}
	return sess.CloseSend()
}

// CloseAllStreams 由应用 shutdown 调用，释放活动连接与仍在 reflection 的打开请求。
func CloseAllStreams() { defaultStreamMgr.closeAll() }

func nowMillis() int64 { return time.Now().UnixNano() / int64(time.Millisecond) }

// isEOF 检测流正常结束。优先用 errors.Is(io.EOF)；grpc-go 会把 server trailer
// 的 status 包装成 status.Error，所以再 status.FromError 看 code（Cancelled/OK 视作流结束）。
func isEOF(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	if st, ok := status.FromError(err); ok {
		// OK / Canceled 都视作"流正常结束"——bidi 在 CloseSend 后服务端 cancel 也走此分支
		if st.Code() == codes.OK || st.Code() == codes.Canceled {
			return true
		}
	}
	// 兜底：极少数场景下 grpc-go 直接传一个 errorString("EOF") 而非 io.EOF
	return err.Error() == "EOF"
}
