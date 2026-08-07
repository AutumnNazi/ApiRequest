package binding

import (
	"context"
	"net/http"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"

	"apirequest/backend/auth"
)

// OAuth2Api OAuth 2.0 token 获取域
type OAuth2Api struct {
	ctx     context.Context
	manager *auth.TokenManager
}

// NewOAuth2Api 构造。浏览器打开延迟到调用时（ctx 由 startup 注入）
func NewOAuth2Api(clients ...*http.Client) *OAuth2Api {
	api := &OAuth2Api{}
	client := http.DefaultClient
	if len(clients) > 0 && clients[0] != nil {
		client = clients[0]
	}
	api.manager = auth.NewTokenManager(func(url string) error {
		if api.ctx == nil {
			return nil
		}
		wailsrt.BrowserOpenURL(api.ctx, url)
		return nil
	}, client)
	return api
}

func (a *OAuth2Api) startup(ctx context.Context) { a.ctx = ctx }

// GetOAuth2Token 按 auth 参数获取 token（缓存/刷新/完整流程自动选择）。
// 授权码模式会拉起系统浏览器等待回调（最长 120s）。
func (a *OAuth2Api) GetOAuth2Token(params map[string]string) (*auth.Token, error) {
	return a.manager.GetToken(context.Background(), params)
}

// ClearOAuth2Token 清除该配置的缓存 token
func (a *OAuth2Api) ClearOAuth2Token(params map[string]string) error {
	a.manager.ClearToken(params)
	return nil
}
