package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// fakeTokenServer 模拟 token 端点
func fakeTokenServer(t *testing.T, expectGrant string, expiresIn int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		if g := form.Get("grant_type"); g != expectGrant {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]string{"error": "unsupported_grant_type", "error_description": g})
			return
		}
		resp := map[string]any{
			"access_token": "AT-" + expectGrant,
			"token_type":   "Bearer",
		}
		if expiresIn > 0 {
			resp["expires_in"] = expiresIn
		}
		if expectGrant == "authorization_code" || expectGrant == "password" {
			resp["refresh_token"] = "RT-1"
		}
		json.NewEncoder(w).Encode(resp)
	}))
}

func TestClientCredentials(t *testing.T) {
	srv := fakeTokenServer(t, "client_credentials", 3600)
	defer srv.Close()

	m := NewTokenManager(nil)
	tok, err := m.GetToken(context.Background(), map[string]string{
		"grantType": "client_credentials",
		"tokenUrl":  srv.URL,
		"clientId":  "c1", "clientSecret": "s1",
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if tok.AccessToken != "AT-client_credentials" || tok.ExpiresAt == 0 {
		t.Errorf("tok = %+v", tok)
	}
}

func TestTokenCacheAndClear(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		json.NewEncoder(w).Encode(map[string]any{"access_token": "AT", "expires_in": 3600})
	}))
	defer srv.Close()

	p := map[string]string{"grantType": "client_credentials", "tokenUrl": srv.URL, "clientId": "c"}
	m := NewTokenManager(nil)
	m.GetToken(context.Background(), p)
	m.GetToken(context.Background(), p) // 应命中缓存
	if calls != 1 {
		t.Errorf("calls = %d, want 1 (cache hit)", calls)
	}
	m.ClearToken(p)
	m.GetToken(context.Background(), p)
	if calls != 2 {
		t.Errorf("calls = %d, want 2 after clear", calls)
	}
}

func TestPasswordGrant(t *testing.T) {
	srv := fakeTokenServer(t, "password", 60)
	defer srv.Close()
	m := NewTokenManager(nil)
	tok, err := m.GetToken(context.Background(), map[string]string{
		"grantType": "password", "tokenUrl": srv.URL,
		"clientId": "c", "username": "u", "password": "p",
	})
	if err != nil {
		t.Fatal(err)
	}
	if tok.RefreshToken != "RT-1" {
		t.Errorf("tok = %+v", tok)
	}
}

func TestTokenErrorSurface(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client", "error_description": "bad secret"})
	}))
	defer srv.Close()
	m := NewTokenManager(nil)
	_, err := m.GetToken(context.Background(), map[string]string{
		"grantType": "client_credentials", "tokenUrl": srv.URL, "clientId": "c",
	})
	if err == nil || !strings.Contains(err.Error(), "invalid_client") {
		t.Errorf("err = %v", err)
	}
}

// TestAuthorizationCodePKCE 完整授权码流程：
// 用模拟浏览器（直接 GET 授权 URL 并跟随重定向到本地回调）代替真实浏览器
func TestAuthorizationCodePKCE(t *testing.T) {
	var gotChallenge, gotVerifier string
	// 授权端点：记录 challenge，302 回 redirect_uri 带 code+state
	authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		gotChallenge = q.Get("code_challenge")
		redirect := q.Get("redirect_uri") + "?code=CODE1&state=" + url.QueryEscape(q.Get("state"))
		http.Redirect(w, r, redirect, 302)
	}))
	defer authSrv.Close()

	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		gotVerifier = form.Get("code_verifier")
		if form.Get("code") != "CODE1" {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"access_token": "AT-code", "expires_in": 3600})
	}))
	defer tokenSrv.Close()

	// "浏览器"：GET 授权 URL，http.Client 自动跟随 302 到本地回调
	openBrowser := func(u string) error {
		go func() {
			resp, err := http.Get(u)
			if err == nil {
				resp.Body.Close()
			}
		}()
		return nil
	}

	m := NewTokenManager(openBrowser)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tok, err := m.GetToken(ctx, map[string]string{
		"grantType": "authorization_code",
		"authUrl":   authSrv.URL,
		"tokenUrl":  tokenSrv.URL,
		"clientId":  "public-client",
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if tok.AccessToken != "AT-code" {
		t.Errorf("tok = %+v", tok)
	}
	// PKCE 校验：challenge 应等于 S256(verifier)
	if gotChallenge == "" || gotVerifier == "" || pkceS256(gotVerifier) != gotChallenge {
		t.Errorf("PKCE mismatch: challenge=%s verifier=%s", gotChallenge, gotVerifier)
	}
}
