package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"apirequest/backend/model"
)

// OAuth 2.0（docs/auth.md §2）。
// oauth2 Provider 的 Apply 只注入已获取的 token；token 获取由 TokenManager
// 走各授权模式（Client Credentials / Password / Authorization Code + PKCE）。

// oauth2Auth 发送时注入缓存的 access token
type oauth2Auth struct{}

func (oauth2Auth) Type() string { return "oauth2" }

func (oauth2Auth) Apply(req *http.Request, p map[string]string) error {
	// 优先用已缓存 token（binding 层 GetToken 后写入 params["accessToken"]）
	token := p["accessToken"]
	if token == "" {
		return model.NewError(model.KindValidation, "no OAuth2 access token; click 获取 Token first")
	}
	prefix := p["headerPrefix"]
	if prefix == "" {
		prefix = "Bearer"
	}
	req.Header.Set("Authorization", prefix+" "+token)
	return nil
}

func init() { Register(oauth2Auth{}) }

// Token 一次获取的 token 结果
type Token struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken,omitempty"`
	TokenType    string `json:"tokenType,omitempty"`
	ExpiresAt    int64  `json:"expiresAt,omitempty"` // Unix ms，0 = 未知
	Scope        string `json:"scope,omitempty"`
}

// TokenStore persists tokens outside collection data. Implementations must
// protect both access and refresh tokens as credentials.
type TokenStore interface {
	Load(key string) (*Token, error)
	Save(key string, token *Token) error
	Delete(key string) error
}

// Expired token 是否已过期（留 30s 余量）
func (t *Token) Expired() bool {
	return t.ExpiresAt > 0 && time.Now().UnixMilli() > t.ExpiresAt-30_000
}

// TokenManager 按配置指纹缓存 token（docs/auth.md：缓存键 = auth 配置指纹）
type TokenManager struct {
	mu         sync.Mutex
	cache      map[string]*Token
	client     *http.Client
	tokenStore TokenStore
	// inflight 按指纹去重：同指纹并发 GetToken 共享同一次授权流程
	// （否则会拉起两个浏览器 + 两个本地回调监听器互相覆盖）
	inflight map[string]*tokenCall
	// OpenBrowser 打开系统浏览器（授权码模式；由 platform 注入，测试可替换）
	OpenBrowser func(url string) error
}

// tokenCall 一次进行中的授权流程（多个等待者共享）
type tokenCall struct {
	done chan struct{}
	tok  *Token
	err  error
}

// NewTokenManager 构造
func NewTokenManager(openBrowser func(string) error, clients ...*http.Client) *TokenManager {
	return NewTokenManagerWithStore(openBrowser, nil, clients...)
}

// NewTokenManagerWithStore constructs a manager backed by an optional secure
// store. Tests and headless callers can still omit persistence.
func NewTokenManagerWithStore(openBrowser func(string) error, tokenStore TokenStore, clients ...*http.Client) *TokenManager {
	client := &http.Client{Timeout: 30 * time.Second}
	if len(clients) > 0 && clients[0] != nil {
		client = clients[0]
	}
	return &TokenManager{
		cache:       map[string]*Token{},
		inflight:    map[string]*tokenCall{},
		client:      client,
		tokenStore:  tokenStore,
		OpenBrowser: openBrowser,
	}
}

// fingerprint 配置指纹（不含易变字段）
func fingerprint(p map[string]string) string {
	keys := []string{"grantType", "tokenUrl", "authUrl", "clientId", "scope", "username"}
	var parts []string
	for _, k := range keys {
		parts = append(parts, p[k])
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return base64.RawURLEncoding.EncodeToString(sum[:16])
}

// GetToken 获取 token：缓存命中且未过期直接返回；过期尝试刷新；否则走授权流程。
// 命名返回值供 inflight defer 兜底回填（见下方去重段）
func (m *TokenManager) GetToken(ctx context.Context, p map[string]string) (tokResult *Token, errResult error) {
	fp := fingerprint(p)
	m.mu.Lock()
	cached := m.cache[fp]
	m.mu.Unlock()
	if cached == nil && m.tokenStore != nil {
		stored, err := m.tokenStore.Load(fp)
		if err != nil {
			return nil, model.WrapError(model.KindStorage, err)
		}
		if stored != nil {
			m.cacheOnly(fp, stored)
			cached = stored
		}
	}

	if cached != nil && !cached.Expired() {
		return cached, nil
	}

	// 同指纹去重：已有授权流程在进行中则直接等待其结果。
	// 必须覆盖 refresh：refresh_token rotation 的服务端（Auth0/Okta 等）会把
	// 旧 token 的二次使用判定为重放攻击并吊销整条会话链，所以并发刷新不能各自发一次
	m.mu.Lock()
	if call, ok := m.inflight[fp]; ok {
		m.mu.Unlock()
		select {
		case <-call.done:
		case <-ctx.Done():
			return nil, model.NewError(model.KindNetwork, "canceled")
		}
		if call.err != nil {
			return nil, call.err
		}
		return call.tok, nil
	}
	call := &tokenCall{done: make(chan struct{})}
	m.inflight[fp] = call
	m.mu.Unlock()
	// 用命名返回值统一填充 call：靠各 return 分支自觉赋值容易漏
	// （曾漏掉非法 grantType 分支，等待者会拿到 (nil, nil)）
	defer func() {
		call.tok, call.err = tokResult, errResult
		if errResult == nil && tokResult == nil {
			// 兜底：任何未来的新分支若两者皆空，等待者也不会收到 nil token + nil error
			call.err = model.NewError(model.KindNetwork, "token request produced no result")
		}
		m.mu.Lock()
		delete(m.inflight, fp)
		m.mu.Unlock()
		close(call.done)
	}()

	// 取得 inflight 所有权后重读缓存：等待期间前一个持有者可能已刷新出新 token，
	// 此时直接复用，不必再打一次网络请求
	m.mu.Lock()
	cached = m.cache[fp]
	m.mu.Unlock()
	if cached != nil && !cached.Expired() {
		return cached, nil
	}

	// 先试 refresh（在去重保护内，同指纹并发只会用掉一次 refresh_token）
	if cached != nil && cached.RefreshToken != "" {
		if tok, err := m.refresh(ctx, p, cached.RefreshToken); err == nil {
			if err := m.put(fp, tok); err != nil {
				return nil, err
			}
			return tok, nil
		}
		// 刷新失败：走完整流程
	}

	var tok *Token
	var err error
	switch p["grantType"] {
	case "client_credentials":
		tok, err = m.clientCredentials(ctx, p)
	case "password":
		tok, err = m.passwordGrant(ctx, p)
	case "authorization_code", "":
		tok, err = m.authorizationCode(ctx, p)
	default:
		return nil, model.NewError(model.KindValidation, "unsupported grant type: "+p["grantType"])
	}
	if err != nil {
		return nil, err
	}
	if err := m.put(fp, tok); err != nil {
		return nil, err
	}
	return tok, nil
}

func (m *TokenManager) put(fp string, tok *Token) error {
	if m.tokenStore != nil {
		if err := m.tokenStore.Save(fp, tok); err != nil {
			return model.WrapError(model.KindStorage, err)
		}
	}
	m.cacheOnly(fp, tok)
	return nil
}

func (m *TokenManager) cacheOnly(fp string, tok *Token) {
	m.mu.Lock()
	m.cache[fp] = tok
	m.mu.Unlock()
}

// ClearToken 清除缓存（前端"清除 Token"）
func (m *TokenManager) ClearToken(p map[string]string) error {
	fp := fingerprint(p)
	if m.tokenStore != nil {
		if err := m.tokenStore.Delete(fp); err != nil {
			return model.WrapError(model.KindStorage, err)
		}
	}
	m.mu.Lock()
	delete(m.cache, fp)
	m.mu.Unlock()
	return nil
}

// ── 各授权模式 ──

func (m *TokenManager) clientCredentials(ctx context.Context, p map[string]string) (*Token, error) {
	form := url.Values{"grant_type": {"client_credentials"}}
	if p["scope"] != "" {
		form.Set("scope", p["scope"])
	}
	return m.tokenRequest(ctx, p, form)
}

func (m *TokenManager) passwordGrant(ctx context.Context, p map[string]string) (*Token, error) {
	form := url.Values{
		"grant_type": {"password"},
		"username":   {p["username"]},
		"password":   {p["password"]},
	}
	if p["scope"] != "" {
		form.Set("scope", p["scope"])
	}
	return m.tokenRequest(ctx, p, form)
}

func (m *TokenManager) refresh(ctx context.Context, p map[string]string, refreshToken string) (*Token, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	return m.tokenRequest(ctx, p, form)
}

// authorizationCode 授权码 + PKCE（docs/auth.md §2 时序）
func (m *TokenManager) authorizationCode(ctx context.Context, p map[string]string) (*Token, error) {
	if p["authUrl"] == "" {
		return nil, model.NewError(model.KindValidation, "authUrl is required for authorization_code")
	}
	// 1. 本地回调服务（127.0.0.1:随机端口）
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, model.WrapError(model.KindNetwork, err)
	}
	redirectUri := fmt.Sprintf("http://%s/callback", ln.Addr().String())

	state, err := randomToken(24)
	if err != nil {
		ln.Close()
		return nil, err
	}
	verifier, err := randomToken(48)
	if err != nil {
		ln.Close()
		return nil, err
	}
	challenge := pkceS256(verifier)

	// 2. 拼授权 URL 并拉起浏览器
	authUrl, err := url.Parse(p["authUrl"])
	if err != nil {
		ln.Close()
		return nil, model.WrapError(model.KindValidation, err)
	}
	q := authUrl.Query()
	q.Set("response_type", "code")
	q.Set("client_id", p["clientId"])
	q.Set("redirect_uri", redirectUri)
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	if p["scope"] != "" {
		q.Set("scope", p["scope"])
	}
	authUrl.RawQuery = q.Encode()

	// 3. 等回调（120s 超时）
	type callbackResult struct {
		code string
		err  error
	}
	resultCh := make(chan callbackResult, 1)
	// 非阻塞投递：主流程只消费一次，多余的回调（双击重试/攻击者并发请求）
	// 直接丢弃，handler goroutine 不会阻塞泄漏
	deliver := func(r callbackResult) {
		select {
		case resultCh <- r:
		default:
		}
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/callback" {
			http.NotFound(w, r)
			return
		}
		q := r.URL.Query()
		if q.Get("state") != state {
			deliver(callbackResult{err: model.NewError(model.KindValidation, "OAuth state mismatch")})
			w.Write([]byte("State mismatch. You can close this window."))
			return
		}
		if e := q.Get("error"); e != "" {
			deliver(callbackResult{err: model.NewError(model.KindNetwork, "authorization denied: "+e)})
			w.Write([]byte("Authorization failed. You can close this window."))
			return
		}
		deliver(callbackResult{code: q.Get("code")})
		w.Write([]byte("Authorization complete. You can close this window and return to ApiRequest."))
	})}
	go srv.Serve(ln)
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		srv.Shutdown(shutdownCtx)
		cancel()
	}()

	if m.OpenBrowser == nil {
		return nil, model.NewError(model.KindValidation, "no browser opener available")
	}
	if err := m.OpenBrowser(authUrl.String()); err != nil {
		return nil, model.WrapError(model.KindNetwork, err)
	}

	var code string
	select {
	case r := <-resultCh:
		if r.err != nil {
			return nil, r.err
		}
		code = r.code
	case <-time.After(120 * time.Second):
		return nil, model.NewError(model.KindNetwork, "OAuth callback timeout (120s)")
	case <-ctx.Done():
		return nil, model.NewError(model.KindNetwork, "canceled")
	}

	// 4. code + verifier 换 token
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectUri},
		"code_verifier": {verifier},
	}
	return m.tokenRequest(ctx, p, form)
}

// tokenRequest POST token 端点（client 凭证按 clientAuth 走 basic 头或 body）
func (m *TokenManager) tokenRequest(ctx context.Context, p map[string]string, form url.Values) (*Token, error) {
	tokenUrl := p["tokenUrl"]
	if tokenUrl == "" {
		return nil, model.NewError(model.KindValidation, "tokenUrl is required")
	}
	if p["clientAuth"] == "body" {
		form.Set("client_id", p["clientId"])
		if p["clientSecret"] != "" {
			form.Set("client_secret", p["clientSecret"])
		}
	} else if form.Get("client_id") == "" && form.Get("grant_type") == "authorization_code" {
		// PKCE 公共客户端：client_id 进 body
		form.Set("client_id", p["clientId"])
	}

	req, err := http.NewRequestWithContext(ctx, "POST", tokenUrl, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, model.WrapError(model.KindValidation, err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if p["clientAuth"] != "body" && p["clientSecret"] != "" {
		req.SetBasicAuth(p["clientId"], p["clientSecret"])
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, model.WrapError(model.KindNetwork, err)
	}
	defer resp.Body.Close()

	var payload struct {
		AccessToken  string      `json:"access_token"`
		RefreshToken string      `json:"refresh_token"`
		TokenType    string      `json:"token_type"`
		ExpiresIn    json.Number `json:"expires_in"`
		Scope        string      `json:"scope"`
		Error        string      `json:"error"`
		ErrorDesc    string      `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, model.NewError(model.KindNetwork, "token endpoint returned non-JSON ("+resp.Status+")")
	}
	if payload.Error != "" || payload.AccessToken == "" {
		detail := payload.Error
		if payload.ErrorDesc != "" {
			detail += ": " + payload.ErrorDesc
		}
		if detail == "" {
			detail = "token endpoint returned no access_token (" + resp.Status + ")"
		}
		return nil, model.NewError(model.KindNetwork, detail)
	}

	tok := &Token{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		TokenType:    payload.TokenType,
		Scope:        payload.Scope,
	}
	if sec, err := payload.ExpiresIn.Int64(); err == nil && sec > 0 {
		tok.ExpiresAt = time.Now().UnixMilli() + sec*1000
	}
	return tok, nil
}

// ── 工具 ──

// randomToken 生成 n 位 base64url 随机串（state / PKCE verifier）。
// 失败必须显式失败：忽略错误会让 b 保持全零，产出可预测的 state/PKCE
// verifier（CSRF 与 PKCE 防护双双失效）。返回错误而非 panic：调用链上
// 没有 recover，panic 会打崩桌面进程
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := randRead(b); err != nil {
		return "", model.NewError(model.KindNetwork, "crypto/rand unavailable: "+err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)[:n], nil
}

func pkceS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
