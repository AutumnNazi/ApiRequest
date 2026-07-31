package binding

import (
	"net/url"
	"strings"
	"time"

	"apirequest/backend/model"
	"apirequest/backend/storage"
)

// attachCookies 把 Jar 中适用的 cookie 合并进请求 Cookie 头。
// 用户已手写 Cookie 头时不覆盖（显式优先）。
func attachCookies(store *storage.Store, req *model.HttpRequest) {
	for _, h := range req.Headers {
		if h.Enabled && strings.EqualFold(h.Key, "Cookie") {
			return
		}
	}
	u, err := url.Parse(req.Url)
	if err != nil || u.Host == "" {
		return
	}
	cookies, err := store.CookiesForHost(u.Hostname())
	if err != nil || len(cookies) == 0 {
		return
	}
	var parts []string
	for _, c := range cookies {
		if c.Secure && u.Scheme != "https" {
			continue
		}
		if c.Path != "" && c.Path != "/" &&
			u.Path != c.Path &&
			!strings.HasPrefix(u.Path, c.Path+"/") {
			continue
		}
		parts = append(parts, c.Name+"="+c.Value)
	}
	if len(parts) > 0 {
		req.Headers = append(req.Headers, model.KV{
			Key: "Cookie", Value: strings.Join(parts, "; "), Enabled: true,
		})
	}
}

// persistCookies 把响应的 Set-Cookie 写回 Jar
func persistCookies(store *storage.Store, reqUrl string, cookies []model.Cookie) {
	if len(cookies) == 0 {
		return
	}
	u, err := url.Parse(reqUrl)
	if err != nil {
		return
	}
	for _, c := range cookies {
		if c.Domain == "" {
			c.Domain = u.Hostname()
		}
		if c.Path == "" {
			c.Path = "/"
		}
		store.UpsertCookie(c)
	}
}

// CookieApi Cookie 管理域
type CookieApi struct {
	store *storage.Store
}

// NewCookieApi 构造
func NewCookieApi(store *storage.Store) *CookieApi { return &CookieApi{store: store} }

// ListCookies 列出 cookie（domain 空 = 全部）
func (a *CookieApi) ListCookies(domain string) ([]model.Cookie, error) {
	out, err := a.store.ListCookies(domain)
	if err != nil {
		return nil, model.WrapError(model.KindStorage, err)
	}
	return out, nil
}

// UpsertCookie 手动新增/编辑 cookie
func (a *CookieApi) UpsertCookie(c model.Cookie) error {
	if c.Domain == "" || c.Name == "" {
		return model.NewError(model.KindValidation, "cookie domain and name are required")
	}
	if c.Expires == 0 {
		// 手动添加默认 30 天，避免被"过期即删"逻辑立即清除
		c.Expires = time.Now().AddDate(0, 0, 30).UnixMilli()
	}
	if err := a.store.UpsertCookie(c); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	return nil
}

// DeleteCookie 删除单个 cookie
func (a *CookieApi) DeleteCookie(domain, path, name string) error {
	if err := a.store.DeleteCookie(domain, path, name); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	return nil
}

// ClearCookies 清空（domain 空 = 全部）
func (a *CookieApi) ClearCookies(domain string) error {
	if err := a.store.ClearCookies(domain); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	return nil
}
