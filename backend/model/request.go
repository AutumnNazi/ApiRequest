// Package model 定义前后端共享的领域模型（契约面）。
// 对应 docs/data-model.md §3；Wails 据此生成 TS 类型，前端做联合类型窄化。
package model

// KV 键值项，用于 query / header / urlencoded 等表格
type KV struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Enabled     bool   `json:"enabled"`
	Description string `json:"description,omitempty"`
}

// Body 请求体。Kind 为判别字段，其余字段按 Kind 生效
// （可辨识联合在 Go 侧摊平为单 struct，前端按 kind 窄化，见 ADR-007）。
type Body struct {
	Kind     string `json:"kind"` // none | raw | formdata | urlencoded | binary | graphql
	Language string `json:"language,omitempty"` // raw: json | xml | html | text
	Text     string `json:"text,omitempty"`     // raw 的内容

	Items []FormItem `json:"items,omitempty"` // formdata / urlencoded

	Path string `json:"path,omitempty"` // binary: 文件路径

	Query     string `json:"query,omitempty"`     // graphql
	Variables string `json:"variables,omitempty"` // graphql: JSON 字符串
}

// FormItem form-data 条目（value 型或 file 型）
type FormItem struct {
	Key     string `json:"key"`
	Type    string `json:"type"` // text | file
	Value   string `json:"value,omitempty"`
	Path    string `json:"path,omitempty"` // file 型的文件路径
	Enabled bool   `json:"enabled"`
}

// Auth 认证配置。Phase 1 仅透传，Phase 3 起由 AuthProvider 消费
type Auth struct {
	Type   string            `json:"type"` // none | basic | bearer | apikey | ...
	Params map[string]string `json:"params,omitempty"`
}

// RequestSettings 请求级设置覆盖项
type RequestSettings struct {
	TimeoutMs       int  `json:"timeoutMs,omitempty"`       // 0 = 默认
	FollowRedirects bool `json:"followRedirects"`
	MaxRedirects    int  `json:"maxRedirects,omitempty"`
	VerifyTLS       bool `json:"verifyTls"`
}

// DefaultSettings 返回新请求的默认设置
func DefaultSettings() RequestSettings {
	return RequestSettings{
		TimeoutMs:       30000,
		FollowRedirects: true,
		MaxRedirects:    10,
		VerifyTLS:       true,
	}
}

// HttpRequest 一次 HTTP 请求的完整描述
type HttpRequest struct {
	Method   string          `json:"method"`
	Url      string          `json:"url"`
	Params   []KV            `json:"params"` // query
	Headers  []KV            `json:"headers"`
	Body     Body            `json:"body"`
	Auth     Auth            `json:"auth"`
	Settings RequestSettings `json:"settings"`
	// 请求级脚本（集合/文件夹级的在 Node 上，执行时继承合并）
	PreScript  string `json:"preScript,omitempty"`
	TestScript string `json:"testScript,omitempty"`
}

// SendContext 发送上下文（前端组装后传给 SendRequest）
type SendContext struct {
	RequestId         string            `json:"requestId,omitempty"`     // 关联的 node id（可空：未保存的草稿）
	WorkspaceId       string            `json:"workspaceId"`
	EnvironmentId     string            `json:"environmentId,omitempty"`
	VariableOverrides map[string]string `json:"variableOverrides,omitempty"`
}

// Cookie 响应中解析出的 cookie
type Cookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain,omitempty"`
	Path     string `json:"path,omitempty"`
	Expires  int64  `json:"expires,omitempty"` // Unix ms，0 = session
	HttpOnly bool   `json:"httpOnly"`
	Secure   bool   `json:"secure"`
}

// Timing 分阶段计时（毫秒）
type Timing struct {
	DnsMs      float64 `json:"dnsMs"`
	ConnectMs  float64 `json:"connectMs"`
	TlsMs      float64 `json:"tlsMs"`
	TtfbMs     float64 `json:"ttfbMs"`
	DownloadMs float64 `json:"downloadMs"`
	TotalMs    float64 `json:"totalMs"`
}

// ResponseBody 响应体：小 body 内联，大 body 给 blob 引用
type ResponseBody struct {
	Inline  bool   `json:"inline"`
	Text    string `json:"text,omitempty"`    // inline=true 时有效
	BlobRef string `json:"blobRef,omitempty"` // inline=false 时的 blobs/ 相对路径
}

// TestResult 单条测试断言结果
type TestResult struct {
	Name  string `json:"name"`
	Pass  bool   `json:"pass"`
	Error string `json:"error,omitempty"`
}

// ResponseResult SendRequest 的返回值
type ResponseResult struct {
	Status      int          `json:"status"`
	StatusText  string       `json:"statusText"`
	Headers     []KV         `json:"headers"`
	Cookies     []Cookie     `json:"cookies"`
	Body        ResponseBody `json:"body"`
	Timing      Timing       `json:"timing"`
	SizeBytes   int64        `json:"sizeBytes"`
	TestResults []TestResult `json:"testResults"`
	ScriptLogs  []string     `json:"scriptLogs"`
	HistoryId   string       `json:"historyId,omitempty"` // 本次发送落库的历史记录 id
}
