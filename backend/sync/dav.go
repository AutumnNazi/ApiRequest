// Package sync 实现基于 WebDAV 的可选同步（docs/sync.md）。
// 用户自带 WebDAV 服务（坚果云/Nextcloud/自建），无需项目方服务端 ——
// 与 Joplin/Floccus 等开源工具同一模式：远端存快照文件，实体级 LWW 合并。
package sync

import (
	"bytes"
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

func (c *davClient) do(method, rel string, body io.Reader) (*http.Response, error) {
	ref, err := url.Parse(rel)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(method, c.base.ResolveReference(ref).String(), body)
	if err != nil {
		return nil, err
	}
	if c.auth != "" {
		req.Header.Set("Authorization", c.auth)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, model.WrapError(model.KindNetwork, err)
	}
	return resp, nil
}

// Get 读远端文件；404 返回 (nil, false, nil)
func (c *davClient) Get(rel string) ([]byte, bool, error) {
	resp, err := c.do("GET", rel, nil)
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
func (c *davClient) Put(rel string, data []byte) error {
	if len(data) > maxSnapshotSize {
		return model.NewError(model.KindValidation, "WebDAV snapshot exceeds 64 MiB limit")
	}
	put := func() (*http.Response, error) { return c.do("PUT", rel, bytes.NewReader(data)) }
	resp, err := put()
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusConflict || resp.StatusCode == http.StatusNotFound {
		// 父目录不存在：逐级 MKCOL 后重试一次
		c.mkcolParents(rel)
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

func (c *davClient) mkcolParents(rel string) {
	parts := strings.Split(rel, "/")
	for i := 1; i < len(parts); i++ {
		dir := strings.Join(parts[:i], "/") + "/"
		resp, err := c.do("MKCOL", dir, nil)
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
