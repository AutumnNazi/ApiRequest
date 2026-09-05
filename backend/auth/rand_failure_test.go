package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"apirequest/backend/model"
)

// withFailingRand 注入随机源故障并在测试结束时恢复
func withFailingRand(t *testing.T) {
	t.Helper()
	prev := randRead
	randRead = func(b []byte) (int, error) {
		return 0, errors.New("entropy source depleted")
	}
	t.Cleanup(func() { randRead = prev })
}

func TestDigestRandomSourceFailureReturnsError(t *testing.T) {
	withFailingRand(t)
	req, _ := http.NewRequest("POST", "https://api.test/x", nil)
	_, err := digestAuth{}.OnChallenge(req,
		`Digest realm="r", nonce="abc", qop="auth"`,
		map[string]string{"username": "u", "password": "p"})
	if err == nil {
		t.Fatal("OnChallenge must fail when the random source fails")
	}
	// KindNetwork 而非 panic：panic 会击穿 engine.send（无 recover）打崩桌面进程
	ae, ok := err.(*model.AppError)
	if !ok || ae.Kind != model.KindNetwork {
		t.Fatalf("err = %#v, want KindNetwork AppError", err)
	}
}

func TestOAuth1RandomSourceFailureReturnsError(t *testing.T) {
	withFailingRand(t)
	req, _ := http.NewRequest("GET", "https://api.test/x", nil)
	err := oauth1Auth{}.Apply(req, map[string]string{
		"consumerKey": "ck", "consumerSecret": "cs",
	})
	if err == nil {
		t.Fatal("Apply must fail when the random source fails")
	}
	ae, ok := err.(*model.AppError)
	if !ok || ae.Kind != model.KindNetwork {
		t.Fatalf("err = %#v, want KindNetwork AppError", err)
	}
	// 失败时不得留下半套好的 Authorization 头
	if req.Header.Get("Authorization") != "" {
		t.Errorf("Authorization header set despite nonce failure: %q", req.Header.Get("Authorization"))
	}
}

func TestOAuth2RandomSourceFailureReturnsError(t *testing.T) {
	withFailingRand(t)
	m := NewTokenManager(nil)
	m.OpenBrowser = func(string) error { return nil } // 不应被调用到
	_, err := m.GetToken(context.Background(), map[string]string{
		"grantType": "authorization_code",
		"authUrl":   "https://auth.test/authorize",
		"tokenUrl":  "https://auth.test/token",
		"clientId":  "c1",
	})
	if err == nil {
		t.Fatal("GetToken must fail when the random source fails")
	}
	ae, ok := err.(*model.AppError)
	if !ok || ae.Kind != model.KindNetwork {
		t.Fatalf("err = %#v, want KindNetwork AppError", err)
	}
	if !strings.Contains(ae.Detail, "random") && !strings.Contains(ae.Detail, "entropy") {
		t.Logf("detail = %q", ae.Detail)
	}
}
