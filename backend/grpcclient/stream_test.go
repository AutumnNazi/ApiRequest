package grpcclient

import (
	"context"
	"errors"
	"io"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TestIsEOF 表驱动覆盖 isEOF 全部分支，防止后续维护误改。
func TestIsEOF(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"io.EOF", io.EOF, true},
		// status.Error(codes.OK, "") 在 grpc-go 实际返回 nil（被视作成功，给 err=nil 路径覆盖）
		{"status OK", status.Error(codes.OK, ""), false},
		{"status Canceled", status.Error(codes.Canceled, ""), true},
		{"status DeadlineExceeded", status.Error(codes.DeadlineExceeded, "context deadline exceeded"), false},
		{"status Unavailable", status.Error(codes.Unavailable, "transport closing"), false},
		// 旧 bug 防回归：含 EOF 子串但非真正 io.EOF 的错误不能被误判为 done
		{"EOF while parsing", errors.New("EOF while parsing"), false},
		{"boom", errors.New("boom"), false},
		// 兜底分支：裸 errorString("EOF") 视作 EOF（兼容 grpc-go 历史行为）
		{"bare EOF string", errors.New("EOF"), true},
		// 包装了 io.EOF 的 fmt.Errorf("wrap: %w", io.EOF) 也应识别
		{"wrapped io.EOF", wrapErr(io.EOF), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isEOF(c.err); got != c.want {
				t.Fatalf("isEOF(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

// wrapErr 模拟 fmt.Errorf("%w", err) 包装，避免在本测试文件引入 fmt 之外的依赖。
func wrapErr(err error) error { return wrappedErr{err: err} }

type wrappedErr struct{ err error }

func (e wrappedErr) Error() string { return "wrapped: " + e.err.Error() }
func (e wrappedErr) Unwrap() error { return e.err }

func TestStreamManagerRejectsDuplicateIDsAndOwnsCleanup(t *testing.T) {
	mgr := newStreamManager()
	_, cancel := context.WithCancel(context.Background())
	token, err := mgr.reserve("same", cancel)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mgr.reserve("same", func() {}); err == nil {
		t.Fatal("duplicate opening session ID was accepted")
	}

	sess := &StreamSession{ID: "same"}
	if !mgr.activate("same", token, sess) {
		t.Fatal("reserved session was not activated")
	}
	if _, err := mgr.reserve("same", func() {}); err == nil {
		t.Fatal("duplicate active session ID was accepted")
	}

	// 旧 goroutine 只能清理它自己，不能按 ID 删除后来者。
	mgr.finish("same", &StreamSession{ID: "same"})
	if got, ok := mgr.get("same"); !ok || got != sess {
		t.Fatal("foreign cleanup removed active session")
	}
	mgr.finish("same", sess)
	if _, ok := mgr.get("same"); ok {
		t.Fatal("owned cleanup did not remove session")
	}
}

func TestStreamSessionCloseSendIsIdempotent(t *testing.T) {
	stream := &fakeClientStream{}
	sess := &StreamSession{Stream: stream}
	if err := sess.CloseSend(); err != nil {
		t.Fatal(err)
	}
	if err := sess.CloseSend(); err != nil {
		t.Fatal(err)
	}
	if err := sess.CloseStream(); err != nil {
		t.Fatal(err)
	}
	if stream.closeCalls != 1 {
		t.Fatalf("CloseSend calls = %d, want 1", stream.closeCalls)
	}
}

func TestStreamManagerCloseAllCancelsOpenAndActive(t *testing.T) {
	mgr := newStreamManager()
	openingCtx, openingCancel := context.WithCancel(context.Background())
	if _, err := mgr.reserve("opening", openingCancel); err != nil {
		t.Fatal(err)
	}
	activeCtx, activeCancel := context.WithCancel(context.Background())
	stream := &fakeClientStream{}
	token, err := mgr.reserve("active", activeCancel)
	if err != nil {
		t.Fatal(err)
	}
	if !mgr.activate("active", token, &StreamSession{ID: "active", Cancel: activeCancel, Stream: stream}) {
		t.Fatal("activate failed")
	}

	mgr.closeAll()
	select {
	case <-openingCtx.Done():
	default:
		t.Fatal("opening session was not canceled")
	}
	select {
	case <-activeCtx.Done():
	default:
		t.Fatal("active session was not canceled")
	}
	if stream.closeCalls != 1 {
		t.Fatalf("active CloseSend calls = %d, want 1", stream.closeCalls)
	}
}

type fakeClientStream struct {
	closeCalls int
}

func (s *fakeClientStream) Header() (metadata.MD, error) { return nil, nil }
func (s *fakeClientStream) Trailer() metadata.MD         { return nil }
func (s *fakeClientStream) CloseSend() error {
	s.closeCalls++
	return nil
}
func (s *fakeClientStream) Context() context.Context { return context.Background() }
func (s *fakeClientStream) SendMsg(any) error        { return nil }
func (s *fakeClientStream) RecvMsg(any) error        { return io.EOF }
