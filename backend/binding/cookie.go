package binding

import (
	"fmt"
	"net/url"
	"strings"

	"apirequest/backend/model"
	"apirequest/backend/storage"
)

// attachCookies 把 Jar 中适用的 cookie 合并进请求 Cookie 头。
// 用户已手写 Cookie 头时不覆盖（显式优先）。
func attachCookies(store *storage.Store, req *model.HttpRequest) error {
	for _, h := range req.Headers {
		if h.Enabled && strings.EqualFold(h.Key, "Cookie") {
			return nil
		}
	}
	u, err := url.Parse(req.Url)
	if err != nil || u.Host == "" {
		return nil
	}
	cookies, err := store.CookiesForHost(u.Hostname())
	if err != nil {
		return err
	}
	if len(cookies) == 0 {
		return nil
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
	return nil
}

// persistCookies 把响应的 Set-Cookie 写回 Jar
func persistCookies(store *storage.Store, reqUrl string, cookies []model.Cookie) error {
	if len(cookies) == 0 {
		return nil
	}
	u, err := url.Parse(reqUrl)
	if err != nil {
		return fmt.Errorf("parse cookie origin %q: %w", reqUrl, err)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("parse cookie origin %q: host is required", reqUrl)
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	normalized := make([]model.Cookie, 0, len(cookies))
	for _, c := range cookies {
		if c.Domain == "" {
			c.Domain = host
			c.HostOnly = true
		} else {
			c.Domain = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(c.Domain)), ".")
			c.HostOnly = false
			if host != c.Domain && !strings.HasSuffix(host, "."+c.Domain) {
				return fmt.Errorf("reject cookie domain %q for host %q", c.Domain, host)
			}
		}
		if c.Path == "" {
			c.Path = defaultCookiePath(u.Path)
		}
		normalized = append(normalized, c)
	}
	return store.UpsertCookies(normalized)
}

func defaultCookiePath(requestPath string) string {
	if requestPath == "" || requestPath[0] != '/' {
		return "/"
	}
	lastSlash := strings.LastIndex(requestPath, "/")
	if lastSlash <= 0 {
		return "/"
	}
	return requestPath[:lastSlash]
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
	if err := validateManagedCookie(c); err != nil {
		return err
	}
	if err := a.store.UpsertCookie(c); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	return nil
}

// UpsertCookies validates and commits an import batch atomically.
func (a *CookieApi) UpsertCookies(cookies []model.Cookie) error {
	for _, cookie := range cookies {
		if err := validateManagedCookie(cookie); err != nil {
			return err
		}
	}
	if err := a.store.UpsertCookies(cookies); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	return nil
}

func validateManagedCookie(cookie model.Cookie) error {
	if strings.TrimSpace(cookie.Domain) == "" || strings.TrimSpace(cookie.Name) == "" {
		return model.NewError(model.KindValidation, "cookie domain and name are required")
	}
	switch strings.ToLower(strings.TrimSpace(cookie.SameSite)) {
	case "", "lax", "strict":
		return nil
	case "none":
		if !cookie.Secure {
			return model.NewError(model.KindValidation, "SameSite=None cookie must be Secure")
		}
		return nil
	default:
		return model.NewError(model.KindValidation, "cookie SameSite must be lax, strict, or none")
	}
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
