package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// 同指纹并发 GetToken 必须由 inflight 去重：只打一次 token 端点，全部调用方共享结果。
// 否则授权码模式会同时拉起多个浏览器窗口、多个本地回调监听器互相覆盖。
//
// 红色验证过：注掉 GetToken 里的 inflight 等待分支后，本用例报 hits = 6。
func TestInflightDedupesConcurrentRequests(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		// 留出窗口让后续调用方进入等待分支而非各自发起请求
		time.Sleep(150 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"AT-shared","token_type":"Bearer","expires_in":3600}`))
	}))
	defer srv.Close()

	params := map[string]string{
		"grantType": "client_credentials",
		"tokenUrl":  srv.URL,
		"clientId":  "c1", "clientSecret": "s1",
	}

	m := NewTokenManager(nil)
	var wg sync.WaitGroup
	const callers = 6
	toks := make([]*Token, callers)
	errs := make([]error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			toks[idx], errs[idx] = m.GetToken(context.Background(), params)
		}(i)
		// 错开启动，确保有调用方落到 inflight 等待分支
		time.Sleep(10 * time.Millisecond)
	}
	wg.Wait()

	for i := 0; i < callers; i++ {
		if errs[i] != nil {
			t.Fatalf("caller %d: %v", i, errs[i])
		}
		if toks[i] == nil {
			t.Fatalf("caller %d got (nil token, nil error)", i)
		}
		if toks[i].AccessToken != "AT-shared" {
			t.Errorf("caller %d token = %q, want AT-shared", i, toks[i].AccessToken)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Errorf("token endpoint hits = %d, want 1 (concurrent same-fingerprint calls must dedupe)", hits)
	}
}

// 过期 token + 同指纹并发：refresh_token 只能被用掉一次。
//
// refresh_token rotation 的服务端（Auth0/Okta 等）把旧 refresh_token 的二次使用
// 判定为重放攻击，会吊销整条会话链。所以 refresh 必须在 inflight 去重保护之内。
func TestConcurrentRefreshUsesRefreshTokenOnce(t *testing.T) {
	var mu sync.Mutex
	var seenRefreshTokens []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		mu.Lock()
		seenRefreshTokens = append(seenRefreshTokens, r.Form.Get("refresh_token"))
		mu.Unlock()
		// 留出窗口让后续调用方进入 inflight 等待分支
		time.Sleep(150 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"access_token":"AT-refreshed","refresh_token":"RT-rotated",` +
			`"token_type":"Bearer","expires_in":3600}`))
	}))
	defer srv.Close()

	params := map[string]string{
		"grantType": "client_credentials",
		"tokenUrl":  srv.URL,
		"clientId":  "c1", "clientSecret": "s1",
	}

	m := NewTokenManager(nil)
	// 预置一个已过期但带 refresh_token 的缓存条目，迫使 GetToken 走刷新路径
	m.cacheOnly(fingerprint(params), &Token{
		AccessToken:  "AT-stale",
		RefreshToken: "RT-original",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(-time.Hour).UnixMilli(),
	})

	var wg sync.WaitGroup
	const callers = 5
	toks := make([]*Token, callers)
	errs := make([]error, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			toks[idx], errs[idx] = m.GetToken(context.Background(), params)
		}(i)
		time.Sleep(10 * time.Millisecond)
	}
	wg.Wait()

	for i := 0; i < callers; i++ {
		if errs[i] != nil {
			t.Fatalf("caller %d: %v", i, errs[i])
		}
		if toks[i] == nil {
			t.Fatalf("caller %d got (nil token, nil error)", i)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(seenRefreshTokens) != 1 {
		t.Fatalf("token endpoint calls = %d %v, want exactly 1 "+
			"(rotation servers revoke the session chain when a refresh_token is reused)",
			len(seenRefreshTokens), seenRefreshTokens)
	}
	if seenRefreshTokens[0] != "RT-original" {
		t.Errorf("refresh_token sent = %q, want RT-original", seenRefreshTokens[0])
	}
}
