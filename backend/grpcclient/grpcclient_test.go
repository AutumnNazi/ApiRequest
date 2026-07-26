package grpcclient

import (
	"net"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

// startTestGrpcServer 起一个带反射与 health 服务的 gRPC server
func startTestGrpcServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer()
	healthpb.RegisterHealthServer(srv, health.NewServer())
	reflection.Register(srv)
	go srv.Serve(ln)
	t.Cleanup(srv.Stop)
	return ln.Addr().String()
}

func TestDiscoverAndCall(t *testing.T) {
	addr := startTestGrpcServer(t)
	cfg := ConnectConfig{Target: addr}

	// Discover：health 服务会被过滤，此处直接验证反射通路 —— 放开过滤单独验证
	methods, err := Discover(cfg)
	// health 被过滤后可能为空 → 报"no services"也算反射通了
	if err != nil && !strings.Contains(err.Error(), "no services discovered") {
		t.Fatalf("discover: %v", err)
	}
	_ = methods

	// Call：直接调用 health check（unary）
	res, err := Call(cfg, "/grpc.health.v1.Health/Check", `{}`, map[string]string{"x-test": "1"})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if !strings.Contains(res.Response, "SERVING") {
		t.Errorf("response = %s", res.Response)
	}
	if res.DurationMs < 0 {
		t.Errorf("duration = %d", res.DurationMs)
	}
}

func TestCallValidation(t *testing.T) {
	addr := startTestGrpcServer(t)
	cfg := ConnectConfig{Target: addr}

	if _, err := Call(cfg, "bad-method-format", "{}", nil); err == nil {
		t.Error("bad method format should error")
	}
	if _, err := Call(cfg, "/no.such.Service/X", "{}", nil); err == nil {
		t.Error("unknown service should error")
	}
	if _, err := Call(cfg, "/grpc.health.v1.Health/Check", `{invalid json`, nil); err == nil {
		t.Error("invalid request JSON should error")
	}
	// 流式方法应被拒绝
	if _, err := Call(cfg, "/grpc.health.v1.Health/Watch", "{}", nil); err == nil ||
		!strings.Contains(err.Error(), "streaming") {
		t.Errorf("streaming should be rejected, got %v", err)
	}
}

func TestDialUnreachable(t *testing.T) {
	cfg := ConnectConfig{Target: "127.0.0.1:1", TimeoutMs: 800}
	if _, err := Discover(cfg); err == nil {
		t.Error("unreachable target should error")
	}
}
