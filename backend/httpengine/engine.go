// Package httpengine 是"解析后的请求 → 响应结果 + 计时"的纯执行单元，
// 不感知集合/变量等上层概念（docs/request-lifecycle.md §4）。
package httpengine

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"apirequest/backend/auth"
	"apirequest/backend/model"
	"apirequest/backend/requesturl"
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

const maxTLSMaterialSize = 4 << 20

// Progress is request-scoped transfer progress reported by SendWithProgress.
type Progress struct {
	Phase         string `json:"phase"` // ttfb | downloading
	BytesReceived int64  `json:"bytesReceived"`
	TotalBytes    int64  `json:"totalBytes,omitempty"`
}

type ProgressFunc func(Progress)

// Engine 持有共享的 http.Transport（连接池）与 blobs 目录
type Engine struct {
	transportMu sync.RWMutex
	transport   *http.Transport
	blobsDir    string // 空 = 不落盘（超限截断），非空 = 超限写 blob
	// proxyOverride 应用级代理设置（nil = 系统代理）
	proxyMu   sync.RWMutex
	proxyFunc func(*http.Request) (*url.URL, error)
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
	switch mode {
	case "none":
		e.proxyFunc = nil
	case "manual":
		u, err := url.Parse(proxyUrl)
		if err != nil || u.Host == "" {
			e.proxyMu.Unlock()
			return model.NewError(model.KindValidation, "invalid proxy url: "+proxyUrl)
		}
		e.proxyFunc = http.ProxyURL(u)
	default: // system
		e.proxyFunc = http.ProxyFromEnvironment
	}
	e.proxyMu.Unlock()
	// 代理变更后关闭旧连接，避免复用到旧代理的连接
	e.currentTransport().CloseIdleConnections()
	return nil
}

// TLSSettings 自定义 TLS 配置（docs/ops.md：允许自定义 CA 与客户端证书）
type TLSSettings struct {
	CaCertPath     string `json:"caCertPath,omitempty"`     // 追加信任的 CA PEM 文件
	ClientCertPath string `json:"clientCertPath,omitempty"` // mTLS 客户端证书 PEM
	ClientKeyPath  string `json:"clientKeyPath,omitempty"`  // mTLS 私钥 PEM
}

// SetTLS 应用 TLS 设置到共享 Transport（对全部请求生效）
func (e *Engine) SetTLS(s TLSSettings) error {
	cfg := &tls.Config{}
	if s.CaCertPath != "" {
		pem, err := readBoundedFile(s.CaCertPath, maxTLSMaterialSize)
		if err != nil {
			return model.NewError(model.KindTls, "read CA cert: "+err.Error())
		}
		// 在系统根证书基础上追加，而非替换
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return model.NewError(model.KindTls, "no valid certificates in "+s.CaCertPath)
		}
		cfg.RootCAs = pool
	}
	if s.ClientCertPath != "" || s.ClientKeyPath != "" {
		if s.ClientCertPath == "" || s.ClientKeyPath == "" {
			return model.NewError(model.KindTls, "client cert and key must both be set")
		}
		certPEM, err := readBoundedFile(s.ClientCertPath, maxTLSMaterialSize)
		if err != nil {
			return model.NewError(model.KindTls, "read client cert: "+err.Error())
		}
		keyPEM, err := readBoundedFile(s.ClientKeyPath, maxTLSMaterialSize)
		if err != nil {
			return model.NewError(model.KindTls, "read client key: "+err.Error())
		}
		cert, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			return model.NewError(model.KindTls, "load client cert: "+err.Error())
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	if s.CaCertPath == "" && s.ClientCertPath == "" {
		cfg = nil // 清除自定义配置，回到默认
	}
	e.transportMu.Lock()
	oldTransport := e.transport
	nextTransport := oldTransport.Clone()
	nextTransport.TLSClientConfig = cfg
	e.transport = nextTransport
	e.transportMu.Unlock()
	oldTransport.CloseIdleConnections()
	return nil
}

func readBoundedFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("path is not a regular file")
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("file exceeds %d MiB limit", limit>>20)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file exceeds %d MiB limit", limit>>20)
	}
	return data, nil
}

func (e *Engine) currentTransport() *http.Transport {
	e.transportMu.RLock()
	defer e.transportMu.RUnlock()
	return e.transport
}

type engineRoundTripper struct{ engine *Engine }

func (transport engineRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport.engine.currentTransport().RoundTrip(request)
}

// NewHTTPClient returns a client that observes later proxy and TLS changes.
// A zero timeout is appropriate for streaming protocols that use contexts.
func (e *Engine) NewHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{Transport: engineRoundTripper{engine: e}, Timeout: timeout}
}

// Send 执行请求。ctx 取消即中止（对应 CancelRequest）。
func (e *Engine) Send(ctx context.Context, req model.HttpRequest) (model.ResponseResult, error) {
	return e.send(ctx, req, nil)
}

// SendWithProgress executes a request and reports final-response transfer progress.
func (e *Engine) SendWithProgress(ctx context.Context, req model.HttpRequest, progress ProgressFunc) (model.ResponseResult, error) {
	return e.send(ctx, req, progress)
}

func (e *Engine) send(ctx context.Context, req model.HttpRequest, progress ProgressFunc) (model.ResponseResult, error) {
	var res model.ResponseResult

	httpReq, err := e.buildRequest(ctx, req)
	if err != nil {
		return res, model.WrapError(model.KindValidation, err)
	}
	if err := auth.Apply(httpReq, req.Auth); err != nil {
		if httpReq.Body != nil {
			_ = httpReq.Body.Close()
		}
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
				if retryReq.Body != nil {
					_ = retryReq.Body.Close()
				}
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
	totalBytes := resp.ContentLength
	if totalBytes < 0 {
		totalBytes = 0
	}
	if progress != nil {
		progress(Progress{Phase: "ttfb", TotalBytes: totalBytes})
	}

	// 流式读 body：前 2MiB 进内存，超限部分边收边写 blob（有 blobsDir 时）
	bodyReader := io.Reader(resp.Body)
	var tracker *downloadProgressReader
	if progress != nil {
		tracker = &downloadProgressReader{reader: resp.Body, total: totalBytes, emit: progress}
		bodyReader = tracker
	}
	body, size, blobRef, err := e.readBodyWithBlob(bodyReader)
	if tracker != nil {
		tracker.finish()
	}
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
	isText := responseBodyIsText(resp.Header.Get("Content-Type"), body)
	if blobRef != "" {
		res.Body = model.ResponseBody{Inline: false, BlobRef: blobRef}
		if isText {
			res.Body.Text = string(body[:min(len(body), 64<<10)]) + "\n… (预览片段，完整响应 " + formatBytes(size) + ")"
			res.Body.Encoding = "utf8"
		} else {
			res.Body.Encoding = "binary"
		}
	} else {
		res.Body = model.ResponseBody{Inline: true}
		if isText {
			res.Body.Text = string(body)
			res.Body.Encoding = "utf8"
		} else {
			res.Body.Text = base64.StdEncoding.EncodeToString(body)
			res.Body.Encoding = "base64"
		}
	}
	res.TestResults = []model.TestResult{}
	res.ScriptLogs = []string{}
	return res, nil
}

type downloadProgressReader struct {
	reader       io.Reader
	total        int64
	received     int64
	lastReported int64
	lastEmit     time.Time
	emit         ProgressFunc
}

func (r *downloadProgressReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 {
		r.received += int64(n)
		if r.received-r.lastReported >= 64<<10 || time.Since(r.lastEmit) >= 50*time.Millisecond {
			r.report()
		}
	}
	return n, err
}

func (r *downloadProgressReader) report() {
	r.lastReported = r.received
	r.lastEmit = time.Now()
	r.emit(Progress{Phase: "downloading", BytesReceived: r.received, TotalBytes: r.total})
}

func (r *downloadProgressReader) finish() {
	if r.lastReported != r.received || r.received == 0 {
		r.report()
	}
}

func responseBodyIsText(contentType string, body []byte) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err == nil && mediaType != "" {
		if strings.HasPrefix(mediaType, "text/") {
			return true
		}
		switch mediaType {
		case "application/json", "application/problem+json", "application/xml",
			"application/xhtml+xml", "application/javascript", "application/graphql",
			"application/x-www-form-urlencoded", "image/svg+xml":
			return true
		}
		if strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml") {
			return true
		}
		return false
	}
	return utf8.Valid(body)
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
	temp, ferr := os.CreateTemp(e.blobsDir, ".response-*.tmp")
	if ferr != nil {
		rest, derr := io.Copy(io.Discard, r)
		total += int64(pn) + rest
		return appendTruncationMarker(buf.Bytes()), total, "", derr // 落盘失败降级为截断，不阻断响应
	}
	tempPath := temp.Name()
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()
	if _, werr := temp.Write(buf.Bytes()); werr != nil {
		rest, derr := io.Copy(io.Discard, r)
		total += int64(pn) + rest
		return appendTruncationMarker(buf.Bytes()), total, "", derr
	}
	if _, werr := temp.Write(probe[:pn]); werr != nil {
		rest, derr := io.Copy(io.Discard, r)
		total += int64(pn) + rest
		return appendTruncationMarker(buf.Bytes()), total, "", derr
	}
	rest, rerr := io.Copy(temp, r)
	total += int64(pn) + rest
	if rerr != nil {
		return buf.Bytes(), total, "", rerr
	}
	if err := temp.Sync(); err != nil {
		return appendTruncationMarker(buf.Bytes()), total, "", nil
	}
	if err := temp.Close(); err != nil {
		return appendTruncationMarker(buf.Bytes()), total, "", nil
	}
	name := uuid.NewString() + ".bin"
	if err := os.Rename(tempPath, filepath.Join(e.blobsDir, name)); err != nil {
		return appendTruncationMarker(buf.Bytes()), total, "", nil
	}
	committed = true
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
	transport := e.currentTransport()
	if !s.VerifyTLS {
		// 关闭校验时克隆 transport，避免污染共享连接池；保留自定义 CA/客户端证书
		t := transport.Clone()
		if t.TLSClientConfig == nil {
			t.TLSClientConfig = &tls.Config{}
		}
		t.TLSClientConfig.InsecureSkipVerify = true
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
	requesturl.AddParams(q, req.Params)
	u.RawQuery = q.Encode()

	body, err := prepareBody(req.Body)
	if err != nil {
		return nil, err
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = "GET"
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, u.String(), body.reader)
	if err != nil {
		if closer, ok := body.reader.(io.Closer); ok {
			_ = closer.Close()
		}
		return nil, err
	}
	if body.getBody != nil {
		httpReq.GetBody = body.getBody
	}
	if body.reader != nil && body.contentLength >= 0 {
		httpReq.ContentLength = body.contentLength
	}
	if body.contentType != "" {
		httpReq.Header.Set("Content-Type", body.contentType)
	}
	for _, h := range req.Headers {
		if h.Enabled && h.Key != "" {
			httpReq.Header.Set(h.Key, h.Value)
		}
	}
	return httpReq, nil
}

type bodyPlan struct {
	reader        io.Reader
	contentType   string
	contentLength int64
	getBody       func() (io.ReadCloser, error)
}

func buildBody(b model.Body) (io.Reader, string, error) {
	body, err := prepareBody(b)
	if err != nil {
		return nil, "", err
	}
	return body.reader, body.contentType, nil
}

func prepareBody(b model.Body) (bodyPlan, error) {
	switch b.Kind {
	case "", "none":
		return bodyPlan{}, nil
	case "raw":
		ct := map[string]string{
			"json": "application/json", "xml": "application/xml",
			"html": "text/html", "text": "text/plain",
		}[b.Language]
		return stringBodyPlan(b.Text, ct), nil
	case "urlencoded":
		form := url.Values{}
		for _, it := range b.Items {
			if it.Enabled && it.Key != "" {
				form.Add(it.Key, it.Value)
			}
		}
		return stringBodyPlan(form.Encode(), "application/x-www-form-urlencoded"), nil
	case "formdata":
		return prepareMultipartBody(b.Items)
	case "binary":
		f, err := os.Open(b.Path)
		if err != nil {
			return bodyPlan{}, fmt.Errorf("binary body: %w", err)
		}
		info, err := f.Stat()
		if err != nil {
			_ = f.Close()
			return bodyPlan{}, fmt.Errorf("binary body: %w", err)
		}
		if !info.Mode().IsRegular() {
			_ = f.Close()
			return bodyPlan{}, errors.New("binary body path is not a regular file")
		}
		path := b.Path
		return bodyPlan{
			reader:        f,
			contentType:   "application/octet-stream",
			contentLength: info.Size(),
			getBody: func() (io.ReadCloser, error) {
				return os.Open(path)
			},
		}, nil
	case "graphql":
		vars := b.Variables
		if strings.TrimSpace(vars) == "" {
			vars = "{}"
		}
		if !json.Valid([]byte(vars)) {
			return bodyPlan{}, fmt.Errorf("graphql variables must be valid JSON")
		}
		payload := fmt.Sprintf(`{"query":%s,"variables":%s}`, jsonString(b.Query), vars)
		return stringBodyPlan(payload, "application/json"), nil
	default:
		return bodyPlan{}, fmt.Errorf("unsupported body kind: %s", b.Kind)
	}
}

func stringBodyPlan(value, contentType string) bodyPlan {
	return bodyPlan{
		reader:        strings.NewReader(value),
		contentType:   contentType,
		contentLength: int64(len(value)),
	}
}

func prepareMultipartBody(items []model.FormItem) (bodyPlan, error) {
	items = append([]model.FormItem(nil), items...)
	for _, item := range items {
		if !item.Enabled || item.Key == "" || item.Type != "file" {
			continue
		}
		info, err := os.Stat(item.Path)
		if err != nil {
			return bodyPlan{}, fmt.Errorf("form file %q: %w", item.Key, err)
		}
		if !info.Mode().IsRegular() {
			return bodyPlan{}, fmt.Errorf("form file %q path is not a regular file", item.Key)
		}
	}

	boundaryWriter := multipart.NewWriter(io.Discard)
	boundary := boundaryWriter.Boundary()
	contentType := boundaryWriter.FormDataContentType()
	open := func() (io.ReadCloser, error) {
		reader, writer := io.Pipe()
		go writeMultipartBody(writer, boundary, items)
		return reader, nil
	}
	reader, err := open()
	if err != nil {
		return bodyPlan{}, err
	}
	return bodyPlan{
		reader:        reader,
		contentType:   contentType,
		contentLength: -1,
		getBody:       open,
	}, nil
}

func writeMultipartBody(pipe *io.PipeWriter, boundary string, items []model.FormItem) {
	writer := multipart.NewWriter(pipe)
	if err := writer.SetBoundary(boundary); err != nil {
		_ = pipe.CloseWithError(err)
		return
	}
	for _, item := range items {
		if !item.Enabled || item.Key == "" {
			continue
		}
		if item.Type != "file" {
			if err := writer.WriteField(item.Key, item.Value); err != nil {
				_ = pipe.CloseWithError(err)
				return
			}
			continue
		}
		file, err := os.Open(item.Path)
		if err != nil {
			_ = pipe.CloseWithError(fmt.Errorf("form file %q: %w", item.Key, err))
			return
		}
		part, err := writer.CreateFormFile(item.Key, filepathBase(item.Path))
		if err == nil {
			_, err = io.Copy(part, file)
		}
		closeErr := file.Close()
		if err == nil {
			err = closeErr
		}
		if err != nil {
			_ = pipe.CloseWithError(err)
			return
		}
	}
	if err := writer.Close(); err != nil {
		_ = pipe.CloseWithError(err)
		return
	}
	_ = pipe.Close()
}

func appendTruncationMarker(head []byte) []byte {
	marker := []byte("\n… (truncated at 2 MiB)")
	out := make([]byte, 0, len(head)+len(marker))
	out = append(out, head...)
	return append(out, marker...)
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
			Expires: exp, MaxAge: c.MaxAge, HttpOnly: c.HttpOnly, Secure: c.Secure,
			SameSite: sameSiteString(c.SameSite), HostOnly: c.Domain == "",
		})
	}
	return out
}

func sameSiteString(value http.SameSite) string {
	switch value {
	case http.SameSiteStrictMode:
		return "strict"
	case http.SameSiteLaxMode:
		return "lax"
	case http.SameSiteNoneMode:
		return "none"
	default:
		return ""
	}
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
