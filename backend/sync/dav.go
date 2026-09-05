// Package sync 实现基于 WebDAV 的可选同步（docs/sync.md）。
// 用户自带 WebDAV 服务（坚果云/Nextcloud/自建），无需项目方服务端 ——
// 与 Joplin/Floccus 等开源工具同一模式：远端存快照文件，实体级 LWW 合并。
package sync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"apirequest/backend/model"
)

const maxSnapshotSize = 64 << 20

// DavConfig WebDAV 连接配置
type DavConfig struct {
	Url           string `json:"url"` // 根地址，如 https://dav.jianguoyun.com/dav/
	Username      string `json:"username"`
	Password      string `json:"password,omitempty"`
	PasswordSet   bool   `json:"passwordSet,omitempty"`
	ClearPassword bool   `json:"clearPassword,omitempty"`
	// OmitSecrets 上传时剥离密钥变量值（docs/sync.md：同步时可选择不上传密钥）
	OmitSecrets bool `json:"omitSecrets"`
}

// davClient 极简 WebDAV 客户端：只用到 GET/PUT/MKCOL
type davClient struct {
	base *url.URL
	http *http.Client
	auth string
}

func newDavClient(cfg DavConfig) (*davClient, error) {
	return newDavClientWithHTTP(cfg, &http.Client{Timeout: 30 * time.Second})
}

func newDavClientWithHTTP(cfg DavConfig, client *http.Client) (*davClient, error) {
	raw := strings.TrimRight(strings.TrimSpace(cfg.Url), "/") + "/"
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, model.NewError(model.KindValidation, "invalid WebDAV url: "+cfg.Url)
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	c := &davClient{base: u, http: client}
	if cfg.Username != "" {
		c.auth = "Basic " + basicToken(cfg.Username, cfg.Password)
	}
	return c, nil
}

// davTransferTimeout 单次 WebDAV 请求的总超时（含 body 传输）：
// 30s 对接近 64 MiB 的快照必然不够，按 body 大小放大（每 MiB 追加 5s，下限 30s）。
// 注意：调用方传入的 *http.Client 不能设 Client.Timeout —— 那是覆盖全程的硬上限，
// 会先于这里派生的 ctx 掐断大快照传输，使本放大逻辑失效
func davTransferTimeout(bodyLen int) time.Duration {
	to := 30 * time.Second
	if extra := (bodyLen >> 20) * 5; extra > 0 {
		to = 30*time.Second + time.Duration(extra)*time.Second
	}
	return to
}

// cancelOnCloseBody 在 Body.Close 时取消请求超时 ctx，
// 避免请求返回即取消导致调用方读 body 中途失败，同时保证超时 ctx 不泄漏
type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

func (c *davClient) do(ctx context.Context, method, rel string, body io.Reader, timeout time.Duration) (*http.Response, error) {
	ref, err := url.Parse(rel)
	if err != nil {
		return nil, err
	}
	var cancel context.CancelFunc
	if timeout > 0 {
		// 请求级超时兜底：client 可能不带 Timeout（engine.NewHTTPClient 共享策略客户端）
		ctx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base.ResolveReference(ref).String(), body)
	if err != nil {
		cancel()
		return nil, err
	}
	if c.auth != "" {
		req.Header.Set("Authorization", c.auth)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// 必须在 cancel() 之前读 ctx.Err()：cancel 会把它置为 Canceled，
		// 之后再判断就无法区分"用户取消/超时"与"真实网络故障"，
		// 后者会被一律误报成已取消，真实原因（连接被拒、DNS、TLS）全部丢失
		ctxErr := ctx.Err()
		cancel()
		// 区分取消与超时：两者都表现为 ctx.Err() 非空，但用户可采取的行动不同
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return nil, model.NewError(model.KindNetwork,
				fmt.Sprintf("WebDAV %s timed out after %s", method, timeout))
		}
		if ctxErr != nil {
			return nil, model.NewError(model.KindNetwork, "sync canceled")
		}
		return nil, model.WrapError(model.KindNetwork, err)
	}
	// 超时窗口覆盖到 body 读完为止，由 Body.Close 收尾取消
	resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

// Get 读远端文件；404 返回 (nil, false, nil)
func (c *davClient) Get(ctx context.Context, rel string) ([]byte, bool, error) {
	// 远端快照大小事先未知，最大可达 maxSnapshotSize，故按上限给超时；
	// 否则拉取大快照会在固定 30s 处被掐断（与 PUT 方向不对称）
	resp, err := c.do(ctx, "GET", rel, nil, davTransferTimeout(maxSnapshotSize))
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, false, nil
	}
	if resp.StatusCode >= 300 {
		return nil, false, davError("GET", rel, resp)
	}
	if resp.ContentLength > maxSnapshotSize {
		return nil, false, model.NewError(model.KindImport, "WebDAV snapshot exceeds 64 MiB limit")
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSnapshotSize+1))
	if err == nil && len(data) > maxSnapshotSize {
		return nil, false, model.NewError(model.KindImport, "WebDAV snapshot exceeds 64 MiB limit")
	}
	return data, true, err
}

// Put 写远端文件；自动补建父目录（MKCOL 幂等）
func (c *davClient) Put(ctx context.Context, rel string, data []byte) error {
	if len(data) > maxSnapshotSize {
		return model.NewError(model.KindValidation, "WebDAV snapshot exceeds 64 MiB limit")
	}
	timeout := davTransferTimeout(len(data))
	put := func() (*http.Response, error) { return c.do(ctx, "PUT", rel, bytes.NewReader(data), timeout) }
	resp, err := put()
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusNotFound {
		// 父目录不存在：逐级 MKCOL 后重试一次
		c.mkcolParents(ctx, rel)
		resp, err = put()
		if err != nil {
			return err
		}
		resp.Body.Close()
	}
	if resp.StatusCode >= 300 {
		return davError("PUT", rel, resp)
	}
	return nil
}

func (c *davClient) mkcolParents(ctx context.Context, rel string) {
	parts := strings.Split(rel, "/")
	for i := 1; i < len(parts); i++ {
		dir := strings.Join(parts[:i], "/") + "/"
		resp, err := c.do(ctx, "MKCOL", dir, nil, davTransferTimeout(0))
		if err == nil {
			resp.Body.Close() // 201 已建 / 405 已存在，都继续
		}
	}
}

func davError(method, rel string, resp *http.Response) error {
	kind := model.KindNetwork
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return model.NewError(kind, fmt.Sprintf("WebDAV auth failed (%s %s → %s); check username/password",
			method, rel, resp.Status))
	}
	return model.NewError(kind, fmt.Sprintf("WebDAV %s %s → %s", method, rel, resp.Status))
}

func basicToken(user, pass string) string {
	return b64(user + ":" + pass)
}
