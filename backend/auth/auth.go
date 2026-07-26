// Package auth 实现认证类型（docs/auth.md）。
// Auth 以"类型 + 参数"建模，在变量解析之后、构造原生请求时应用。
// 每种类型实现 Provider 并注册（docs/extensibility.md）。
package auth

import (
	"fmt"
	"net/http"

	"apirequest/backend/model"
)

// Provider 认证提供者：把凭证应用到即将发送的请求上。
// Digest 等两段式认证需要引擎回调，见 TwoPhaseProvider。
type Provider interface {
	Type() string
	Apply(req *http.Request, params map[string]string) error
}

// TwoPhaseProvider 两段式认证（如 Digest）：第一次 401 响应后重新签名重发
type TwoPhaseProvider interface {
	Provider
	// OnChallenge 收到 401 后基于 WWW-Authenticate 头生成新的 Authorization；
	// 返回 false 表示无法处理（不重发）
	OnChallenge(req *http.Request, challenge string, params map[string]string) (bool, error)
}

var registry = map[string]Provider{}

// Register 注册认证类型（init 时调用）
func Register(p Provider) { registry[p.Type()] = p }

// Get 按类型取 Provider；none/inherit/空返回 nil
func Get(authType string) (Provider, error) {
	switch authType {
	case "", "none", "inherit":
		return nil, nil
	}
	p, ok := registry[authType]
	if !ok {
		return nil, model.NewError(model.KindValidation, fmt.Sprintf("unsupported auth type: %s", authType))
	}
	return p, nil
}

// Apply 便捷入口：应用 auth 到请求（none 时 no-op）
func Apply(req *http.Request, a model.Auth) error {
	p, err := Get(a.Type)
	if err != nil || p == nil {
		return err
	}
	return p.Apply(req, a.Params)
}
