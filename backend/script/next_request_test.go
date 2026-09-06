package script

import (
	"testing"
	"time"

	"apirequest/backend/model"
)

// pm.setNextRequest(name)：测试脚本设置下一次要跑的请求名（Runner 流转控制）
func TestSetNextRequestInResult(t *testing.T) {
	sb := NewSandbox(2*time.Second, map[string]string{}, map[string]string{}, map[string]string{}, map[string]string{})
	sb.SetRequest(&model.HttpRequest{Method: "GET", Url: "https://x.test", Settings: model.DefaultSettings()})
	sb.SetResponse(&model.ResponseResult{Status: 200})
	err := sb.Run(`
		pm.setNextRequest('search items');
		pm.test('status ok', function () { pm.expect(pm.response.code).to.equal(200); });
	`, "test")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	res := sb.Result()
	if res.NextRequest != "search items" {
		t.Fatalf("NextRequest = %q, want 'search items'", res.NextRequest)
	}
}

// 未调用不产生值（保持自然顺序）
func TestNoNextRequestByDefault(t *testing.T) {
	sb := NewSandbox(2*time.Second, map[string]string{}, map[string]string{}, map[string]string{}, map[string]string{})
	sb.SetResponse(&model.ResponseResult{Status: 200})
	if err := sb.Run("pm.test('x', function(){});", "test"); err != nil {
		t.Fatal(err)
	}
	if sb.Result().NextRequest != "" {
		t.Fatalf("NextRequest = %q, want empty", sb.Result().NextRequest)
	}
}

// 空串/null = 清除（回到自然顺序）
func TestClearNextRequest(t *testing.T) {
	sb := NewSandbox(2*time.Second, map[string]string{}, map[string]string{}, map[string]string{}, map[string]string{})
	sb.SetResponse(&model.ResponseResult{Status: 200})
	if err := sb.Run("pm.setNextRequest('a'); pm.setNextRequest(null);", "test"); err != nil {
		t.Fatal(err)
	}
	if sb.Result().NextRequest != "" {
		t.Fatalf("NextRequest = %q, want cleared", sb.Result().NextRequest)
	}
}
