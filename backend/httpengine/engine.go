// Package httpengine 是"解析后的请求 → 响应结果 + 计时"的纯执行单元，
// 不感知集合/变量等上层概念（docs/request-lifecycle.md §4）。
package httpengine

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"apirequest/backend/auth"
	"apirequest/backend/model"
)

// authProviderTwoPhase 判断该类型是否为两段式认证
func authProviderTwoPhase(authType string) (auth.TwoPhaseProvider, bool) {
	p, err := auth.Get(authType)
	if err != nil || p == nil {
		return nil, false
	}
	tp, ok := p.(auth.TwoPhaseProvider)
	return tp, ok
}

// inlineBodyLimit 超过该字节数的响应体不内联返回，落 blobs/ 并返回引用
const inlineBodyLimit = 2 << 20 // 2 MiB

// Engine 持有共享的 http.Transport（连接池）与 blobs 目录
type Engine struct {
	transport *http.Transport
	blobsDir  string // 空 = 不落盘（超限截断），非空 = 超限写 blob
	// proxyOverride 应用级代理设置（nil = 系统代理）
	proxyMu    sync.RWMutex
	proxyFunc  func(*http.Request) (*url.URL, error)
}

// New 创建引擎
func New() *Engine {
	e := &Engine{
		transport: &http.Transport{
			MaxIdleConnsPerHost: 8,
			IdleConnTimeout:     90 * time.Second,
			ForceAttemptHTTP2:   true,
		},
	}
	e.proxyFunc = http.ProxyFromEnvironment
	e.transport.Proxy = func(r *http.Request) (*url.URL, error) {
		e.proxyMu.RLock()
		fn := e.proxyFunc
		e.proxyMu.RUnlock()
		if fn == nil {
			return nil, nil
		}
		return fn(r)
	}
	return e
}

// SetBlobsDir 设置大响应落盘目录（binding 层初始化时注入）
func (e *Engine) SetBlobsDir(dir string) { e.blobsDir = dir }

// SetProxy 应用代理设置。mode: system | manual | none；manual 时用 proxyUrl。
func (e *Engine) SetProxy(mode, proxyUrl string) error {
	e.proxyMu.Lock()
	defer e.proxyMu.Unlock()
	switch mode {
	case "none":
		e.proxyFunc = nil
	case "manual":
		u, err := url.Parse(proxyUrl)
		if err != nil || u.Host == "" {
			return model.NewError(model.KindValidation, "invalid proxy url: "+proxyUrl)
		}
		e.proxyFunc = http.ProxyURL(u)
	default: // system
		e.proxyFunc = http.ProxyFromEnvironment
	}
	// 代理变更后关闭旧连接，避免复用到旧代理的连接
	e.transport.CloseIdleConnections()
	return nil
}

// Send 执行请求。ctx 取消即中止（对应 CancelRequest）。
func (e *Engine) Send(ctx context.Context, req model.HttpRequest) (model.ResponseResult, error) {
	var res model.ResponseResult

	httpReq, err := e.buildRequest(ctx, req)
	if err != nil {
		return res, model.WrapError(model.KindValidation, err)
	}
	if err := auth.Apply(httpReq, req.Auth); err != nil {
		return res, model.WrapError(model.KindValidation, err)
	}

	tr := newTraceTimer()
	httpReq = httpReq.WithContext(httptrace.WithClientTrace(httpReq.Context(), tr.clientTrace()))

	client := e.buildClient(req.Settings)
	start := time.Now()
	resp, err := client.Do(httpReq)
	if err != nil {
		if ctx.Err() == context.Canceled {
			return res, model.NewError(model.KindNetwork, "canceled")
		}
		kind := model.KindNetwork
		if strings.Contains(err.Error(), "tls") || strings.Contains(err.Error(), "certificate") {
			kind = model.KindTls
		}
		return res, model.WrapError(kind, err)
	}

	// 两段式认证（Digest）：401 + 挑战头 → 重新签名重发一次
	if resp.StatusCode == http.StatusUnauthorized {
		if tp, ok := authProviderTwoPhase(req.Auth.Type); ok {
			challenge := resp.Header.Get("WWW-Authenticate")
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()

			retryReq, rerr := e.buildRequest(ctx, req)
			if rerr != nil {
				return res, model.WrapError(model.KindValidation, rerr)
			}
			handled, herr := tp.OnChallenge(retryReq, challenge, req.Auth.Params)
			if herr != nil {
				return res, model.WrapError(model.KindValidation, herr)
			}
			if handled {
				tr = newTraceTimer() // 重置计时：以第二次请求为准
				retryReq = retryReq.WithContext(httptrace.WithClientTrace(retryReq.Context(), tr.clientTrace()))
				start = time.Now()
				resp, err = client.Do(retryReq)
				if err != nil {
					if ctx.Err() == context.Canceled {
						return res, model.NewError(model.KindNetwork, "canceled")
					}
					return res, model.WrapError(model.KindNetwork, err)
				}
			} else {
				// 无法处理挑战：重新请求一次拿回原始 401 响应体
				resp, err = client.Do(retryReq)
				if err != nil {
					return res, model.WrapError(model.KindNetwork, err)
				}
			}
		}
	}
	defer resp.Body.Close()

	// 流式读 body：前 2MiB 进内存，超限部分边收边写 blob（有 blobsDir 时）
	body, size, blobRef, err := e.readBodyWithBlob(resp.Body)
	end := time.Now()
	if err != nil && ctx.Err() == context.Canceled {
		return res, model.NewError(model.KindNetwork, "canceled")
	}
	if err != nil {
		return res, model.WrapError(model.KindNetwork, err)
	}

	res.Status = resp.StatusCode
	res.StatusText = strings.TrimSpace(strings.TrimPrefix(resp.Status, fmt.Sprintf("%d", resp.StatusCode)))
	res.Headers = flattenHeaders(resp.Header)
	res.Cookies = convertCookies(resp.Cookies())
	res.SizeBytes = size
	res.Timing = tr.timing(start, end)
	if blobRef != "" {
		// 大响应：内联预览片段 + blob 引用（前端按需拉全量）
		res.Body = model.ResponseBody{
			Inline: false, BlobRef: blobRef,
			Text: string(body[:min(len(body), 64<<10)]) + "\n… (预览片段，完整响应 " + formatBytes(size) + ")",
		}
	} else {
		res.Body = model.ResponseBody{Inline: true, Text: string(body)}
	}
	res.TestResults = []model.TestResult{}
	res.ScriptLogs = []string{}
	return res, nil
}

// readBodyWithBlob 读响应体：≤限额全内联；超限时（有 blobsDir）全量写 blob 文件，
// 内存只留前 2MiB 作预览；无 blobsDir 时退回丢弃式截断
func (e *Engine) readBodyWithBlob(r io.Reader) (head []byte, total int64, blobRef string, err error) {
	var buf bytes.Buffer
	n, err := io.Copy(&buf, io.LimitReader(r, inlineBodyLimit))
	total = n
	if err != nil {
		return buf.Bytes(), total, "", err
	}
	if n < inlineBodyLimit {
		return buf.Bytes(), total, "", nil
	}
	// 到达限额：还有更多数据吗？
	probe := make([]byte, 1)
	pn, perr := r.Read(probe)
	if pn == 0 && perr == io.EOF {
		return buf.Bytes(), total, "", nil // 恰好等于限额
	}
	if perr != nil && perr != io.EOF {
		return buf.Bytes(), total, "", perr
	}

	if e.blobsDir == "" {
		// 无落盘目录：统计大小后丢弃
		rest, derr := io.Copy(io.Discard, r)
		total += int64(pn) + rest
		b := buf.Bytes()
		return append(b, []byte("\n… (truncated at 2 MiB)")...), total, "", derr
	}

	// 落盘：头部 + probe + 剩余 全量写文件
	name := uuid.NewString() + ".bin"
	f, ferr := os.Create(filepath.Join(e.blobsDir, name))
	if ferr != nil {
		rest, _ := io.Copy(io.Discard, r)
		total += int64(pn) + rest
		return buf.Bytes(), total, "", nil // 落盘失败降级为截断，不阻断响应
	}
	defer f.Close()
	if _, werr := f.Write(buf.Bytes()); werr != nil {
		return buf.Bytes(), total, "", nil
	}
	f.Write(probe[:pn])
	rest, rerr := io.Copy(f, r)
	total += int64(pn) + rest
	if rerr != nil {
		return buf.Bytes(), total, "", rerr
	}
	return buf.Bytes(), total, name, nil
}

func formatBytes(n int64) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	default:
		return fmt.Sprintf("%.2f MB", float64(n)/(1<<20))
	}
}

func (e *Engine) buildClient(s model.RequestSettings) *http.Client {
	transport := e.transport
	if !s.VerifyTLS {
		// 关闭校验时克隆 transport，避免污染共享连接池
		t := e.transport.Clone()
		t.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
		transport = t
	}
	timeout := time.Duration(s.TimeoutMs) * time.Millisecond
	if s.TimeoutMs <= 0 {
		timeout = 30 * time.Second
	}
	client := &http.Client{Transport: transport, Timeout: timeout}
	if !s.FollowRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	} else if s.MaxRedirects > 0 {
		max := s.MaxRedirects
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			if len(via) >= max {
				return fmt.Errorf("stopped after %d redirects", max)
			}
			return nil
		}
	}
	return client
}

func (e *Engine) buildRequest(ctx context.Context, req model.HttpRequest) (*http.Request, error) {
	u, err := url.Parse(strings.TrimSpace(req.Url))
	if err != nil {
		return nil, fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme == "" {
		u.Scheme = "http"
	}
	// 合并编辑器 params 到 query（URL 中已有的保留）
	q := u.Query()
	for _, p := range req.Params {
		if p.Enabled && p.Key != "" {
			q.Add(p.Key, p.Value)
		}
	}
	u.RawQuery = q.Encode()

	bodyReader, contentType, err := buildBody(req.Body)
	if err != nil {
		return nil, err
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = "GET"
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, u.String(), bodyReader)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		httpReq.Header.Set("Content-Type", contentType)
	}
	for _, h := range req.Headers {
		if h.Enabled && h.Key != "" {
			httpReq.Header.Set(h.Key, h.Value)
		}
	}
	return httpReq, nil
}

func buildBody(b model.Body) (io.Reader, string, error) {
	switch b.Kind {
	case "", "none":
		return nil, "", nil
	case "raw":
		ct := map[string]string{
			"json": "application/json", "xml": "application/xml",
			"html": "text/html", "text": "text/plain",
		}[b.Language]
		return strings.NewReader(b.Text), ct, nil
	case "urlencoded":
		form := url.Values{}
		for _, it := range b.Items {
			if it.Enabled && it.Key != "" {
				form.Add(it.Key, it.Value)
			}
		}
		return strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", nil
	case "formdata":
		var buf bytes.Buffer
		w := multipart.NewWriter(&buf)
		for _, it := range b.Items {
			if !it.Enabled || it.Key == "" {
				continue
			}
			if it.Type == "file" {
				f, err := os.Open(it.Path)
				if err != nil {
					return nil, "", fmt.Errorf("form file %q: %w", it.Key, err)
				}
				fw, err := w.CreateFormFile(it.Key, filepathBase(it.Path))
				if err == nil {
					_, err = io.Copy(fw, f)
				}
				f.Close()
				if err != nil {
					return nil, "", err
				}
			} else if err := w.WriteField(it.Key, it.Value); err != nil {
				return nil, "", err
			}
		}
		if err := w.Close(); err != nil {
			return nil, "", err
		}
		return &buf, w.FormDataContentType(), nil
	case "binary":
		f, err := os.Open(b.Path)
		if err != nil {
			return nil, "", fmt.Errorf("binary body: %w", err)
		}
		return f, "application/octet-stream", nil // 流式，client.Do 后由 http 层关闭
	case "graphql":
		vars := b.Variables
		if strings.TrimSpace(vars) == "" {
			vars = "{}"
		}
		payload := fmt.Sprintf(`{"query":%s,"variables":%s}`, jsonString(b.Query), vars)
		return strings.NewReader(payload), "application/json", nil
	default:
		return nil, "", fmt.Errorf("unsupported body kind: %s", b.Kind)
	}
}

func flattenHeaders(h http.Header) []model.KV {
	out := make([]model.KV, 0, len(h))
	for k, vs := range h {
		for _, v := range vs {
			out = append(out, model.KV{Key: k, Value: v, Enabled: true})
		}
	}
	return out
}

func convertCookies(cs []*http.Cookie) []model.Cookie {
	out := make([]model.Cookie, 0, len(cs))
	for _, c := range cs {
		var exp int64
		if !c.Expires.IsZero() {
			exp = c.Expires.UnixMilli()
		}
		out = append(out, model.Cookie{
			Name: c.Name, Value: c.Value, Domain: c.Domain, Path: c.Path,
			Expires: exp, HttpOnly: c.HttpOnly, Secure: c.Secure,
		})
	}
	return out
}

func filepathBase(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		return p[i+1:]
	}
	return p
}

func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}
