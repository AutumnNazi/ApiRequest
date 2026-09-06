// Package updater 实现 ADR-018 的更新验证链（签名校验 + 版本比对）：
// 拉取渠道 manifest 与其 ed25519 签名 → 验签 → 版本/ floor 比对。
// 本包只做"检查与判定"，不下载安装包、不做静默替换——后者按 ADR-018
// 仍属实施排期，当前 Settings 只展示经签名验证的结果与下载页链接。
package updater

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strings"
	"time"

	"apirequest/backend/model"
)

const (
	// StatusAvailable 有新版本且通过验证
	StatusAvailable = "available"
	// StatusUpToDate 已是最新
	StatusUpToDate = "up-to-date"
	// StatusManualRequired 当前版本低于 floor：提示去下载页手动安装，不走静默链路
	StatusManualRequired = "manual-required"
	// StatusVerificationFailed 验签失败：绝不采信内容
	StatusVerificationFailed = "verification-failed"
)

const manifestFetchLimit = 1 << 20

// Manifest 渠道文件（stable.json / beta.json）
type Manifest struct {
	Version    string                   `json:"version"`
	ReleasedAt int64                    `json:"releasedAt,omitempty"`
	NotesURL   string                   `json:"notesUrl,omitempty"`
	Floor      string                   `json:"floor,omitempty"`
	Platforms  map[string]PlatformEntry `json:"platforms"`
}

// PlatformEntry 单平台安装包信息
type PlatformEntry struct {
	URL    string `json:"url,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Size   int64  `json:"size,omitempty"`
}

// Config 检查参数
type Config struct {
	ManifestURL    string // 渠道 manifest 地址
	PublicKey      ed25519.PublicKey
	CurrentVersion string
	Platform       string       // 如 windows-amd64；空 = runtime 推断
	HTTPClient     *http.Client // nil = 默认 15s
}

// CheckResult 检查结论
type CheckResult struct {
	Status        string `json:"status"`
	LatestVersion string `json:"latestVersion,omitempty"`
	NotesURL      string `json:"notesUrl,omitempty"`
	DownloadURL   string `json:"downloadUrl,omitempty"`
	Detail        string `json:"detail,omitempty"`
}

// CompareVersions 语义化版本数值比较（容忍 v 前缀与缺段；不可解析视为相等，
// 由调用方决定"当前版本未知"的语义）。返回 -1/0/1。
func CompareVersions(a, b string) int {
	pa, aOK := parseVersion(a)
	pb, bOK := parseVersion(b)
	if !aOK || !bOK {
		return 0
	}
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		x, y := segAt(pa, i), segAt(pb, i)
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// parseVersion "v1.2.3-beta" → [1,2,3]，末段带预发布后缀则视为低于正式版（附 -1 修正位）
func parseVersion(v string) ([]int, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if v == "" {
		return nil, false
	}
	prerelease := strings.Contains(v, "-")
	v = strings.SplitN(v, "-", 2)[0]
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts)+1)
	for _, p := range parts {
		n := 0
		if p == "" {
			return nil, false
		}
		for _, ch := range p {
			if ch < '0' || ch > '9' {
				return nil, false
			}
			n = n*10 + int(ch-'0')
		}
		out = append(out, n)
	}
	if prerelease {
		out = append(out, -1)
	}
	return out, true
}

func segAt(segs []int, i int) int {
	if i < len(segs) {
		return segs[i]
	}
	return 0
}

// Check 执行一次完整的验证链。任何网络/解析错误返回 error；
// 验签失败不视为网络错误——返回 verification-failed 结果供 UI 明示。
func Check(ctx context.Context, cfg Config) (*CheckResult, error) {
	if cfg.ManifestURL == "" {
		return nil, model.NewError(model.KindValidation, "update manifest url is not configured")
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	body, err := fetch(ctx, client, cfg.ManifestURL)
	if err != nil {
		return nil, err
	}
	sigRaw, err := fetch(ctx, client, cfg.ManifestURL+".sig")
	if err != nil {
		return nil, err
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigRaw)))
	if err != nil {
		return verifyFailedResult("signature is not valid base64"), nil
	}
	if len(cfg.PublicKey) != ed25519.PublicKeySize ||
		!ed25519.Verify(cfg.PublicKey, body, sig) {
		return verifyFailedResult("ed25519 verification failed; manifest rejected"), nil
	}
	var manifest Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, model.NewError(model.KindImport, "update manifest invalid: "+err.Error())
	}
	if manifest.Version == "" {
		return nil, model.NewError(model.KindImport, "update manifest missing version")
	}

	res := &CheckResult{
		Status:        StatusUpToDate,
		LatestVersion: manifest.Version,
		NotesURL:      manifest.NotesURL,
	}
	switch cmp := CompareVersions(manifest.Version, cfg.CurrentVersion); {
	case cmp <= 0:
		res.Detail = fmt.Sprintf("当前版本 %s 已是最新", cfg.CurrentVersion)
		return res, nil
	case manifest.Floor != "" && CompareVersions(cfg.CurrentVersion, manifest.Floor) < 0:
		res.Status = StatusManualRequired
		res.Detail = fmt.Sprintf("当前版本 %s 低于最低自动升级版本 %s，请到下载页手动安装", cfg.CurrentVersion, manifest.Floor)
	default:
		res.Status = StatusAvailable
		if entry, ok := manifest.Platforms[platformKey(cfg.Platform)]; ok {
			res.DownloadURL = entry.URL
		} else {
			res.Detail = "该平台暂无自动更新包，请到下载页获取"
		}
	}
	return res, nil
}

func platformKey(explicit string) string {
	if explicit != "" {
		return explicit
	}
	return runtime.GOOS + "-" + runtime.GOARCH
}

func verifyFailedResult(detail string) *CheckResult {
	return &CheckResult{Status: StatusVerificationFailed, Detail: detail}
}

func fetch(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, model.WrapError(model.KindValidation, err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, model.WrapError(model.KindNetwork, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, model.NewError(model.KindNetwork,
			fmt.Sprintf("update check: GET %s → %s", url, resp.Status))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, manifestFetchLimit+1))
	if err != nil {
		return nil, model.WrapError(model.KindNetwork, err)
	}
	if len(data) > manifestFetchLimit {
		return nil, model.NewError(model.KindImport, "update manifest exceeds 1 MiB limit")
	}
	return data, nil
}
