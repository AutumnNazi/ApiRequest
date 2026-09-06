package mock

import (
	"strings"
	"testing"
)

func TestRunMockScriptRespond(t *testing.T) {
	script := `
		if (mockRequest.query.env[0] === "prod") {
			respond({ status: 200, body: JSON.stringify({ env: "prod", user: mockRequest.queryFirst("user") }) });
		} else {
			respond({ status: 404, headers: { "X-Mock": "stage" }, body: "not found: " + mockRequest.path });
		}
	`
	req := MockRequest{
		Method:  "GET",
		Path:    "/api/users/42",
		Query:   map[string][]string{"env": {"stage"}, "user": {"alice"}},
		Headers: map[string]string{"Authorization": "Bearer x"},
		Body:    "",
	}
	resp, called, err := RunMockScript(script, req)
	if err != nil || !called {
		t.Fatalf("RunMockScript: called=%v err=%v", called, err)
	}
	if resp.Status != 404 {
		t.Fatalf("status = %d, want 404 (stage branch)", resp.Status)
	}
	if resp.Headers["X-Mock"] != "stage" {
		t.Fatalf("headers = %v", resp.Headers)
	}
	if resp.Body != "not found: /api/users/42" {
		t.Fatalf("body = %q", resp.Body)
	}

	req.Query["env"] = []string{"prod"}
	resp, called, err = RunMockScript(script, req)
	if err != nil || !called {
		t.Fatalf("called=%v err=%v", called, err)
	}
	if resp.Status != 200 || !strings.Contains(resp.Body, `"user":"alice"`) {
		t.Fatalf("resp = %+v", resp)
	}
}

// 不调用 respond：回退静态示例（返回 ok=false 且无错误）
func TestRunMockScriptFallback(t *testing.T) {
	resp, called, err := RunMockScript(`console.log("noop");`, MockRequest{})
	if err != nil || called {
		t.Fatalf("called=%v err=%v, want fallback", called, err)
	}
	if resp.Status != 0 {
		t.Fatalf("resp = %+v", resp)
	}
}

// 脚本抛错：返回错误（服务端转 500），不 panic
func TestRunMockScriptThrows(t *testing.T) {
	_, _, err := RunMockScript(`throw new Error("boom");`, MockRequest{})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err = %v, want boom", err)
	}
}

// 死循环：2s 内被中断
func TestRunMockScriptInterrupts(t *testing.T) {
	if testing.Short() {
		t.Skip("skip in -short")
	}
	_, _, err := RunMockScript(`while (true) {}`, MockRequest{})
	if err == nil {
		t.Fatal("infinite script must be interrupted with an error")
	}
}
