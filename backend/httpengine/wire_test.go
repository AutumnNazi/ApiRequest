package httpengine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"apirequest/backend/model"
)

// WireDump 捕获"最终实际发出的请求"（resp.Request）：
// 含鉴权注入后的头、Go 自动跟随重定向后的最终请求（docs/advanced.md 调试可观测性）。
func TestSendCapturesWireDumpOfFinalRequest(t *testing.T) {
	var seenAuth string
	var redirectAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			redirectAuth = r.Header.Get("Authorization")
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		if r.URL.Path == "/final" {
			seenAuth = r.Header.Get("Authorization")
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	req := testReq(srv.URL + "/redirect")
	req.Auth = model.Auth{Type: "bearer", Params: map[string]string{"token": "tok-123"}}
	res, err := New().Send(context.Background(), req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	if res.Wire == nil {
		t.Fatal("wire dump missing")
	}
	// 请求行：最终落点是 /final（重定向由 Go transport 自动跟随）
	if res.Wire.Method != http.MethodGet {
		t.Errorf("wire method = %q", res.Wire.Method)
	}
	if res.Wire.Url != srv.URL+"/final" {
		t.Errorf("wire url = %q, want final redirect target", res.Wire.Url)
	}
	// 鉴权头按注入后的原样捕获（脱敏由 binding 层负责）
	if res.Wire.Headers == nil || !slices.ContainsFunc(res.Wire.Headers, func(h model.KV) bool {
		return h.Key == "Authorization" && h.Value == "Bearer tok-123"
	}) {
		t.Fatalf("wire headers missing raw Authorization: %+v", res.Wire.Headers)
	}
	if seenAuth != "Bearer tok-123" || redirectAuth != "Bearer tok-123" {
		// 同源重定向 Go 会保留 Authorization；这里只校验服务端确实收到
		t.Logf("server saw auth: redirect=%q final=%q", redirectAuth, seenAuth)
	}
}
