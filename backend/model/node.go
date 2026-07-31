package model

// Node 集合树节点：collection / folder / request 统一实体（ADR-004）。
// kind=request 时 Request 字段有效；collection/folder 级的可继承配置放
// Auth/Variables/PreScript/TestScript。
type Node struct {
	Id          string  `json:"id"`
	WorkspaceId string  `json:"workspaceId"`
	ParentId    string  `json:"parentId,omitempty"` // 空 = 集合根
	Kind        string  `json:"kind"`               // collection | folder | request
	Name        string  `json:"name"`
	SortOrder   float64 `json:"sortOrder"`

	Request *HttpRequest `json:"request,omitempty"` // kind=request

	Auth       *Auth      `json:"auth,omitempty"` // 可继承
	Variables  []Variable `json:"variables,omitempty"`
	PreScript  string     `json:"preScript,omitempty"`
	TestScript string     `json:"testScript,omitempty"`

	CreatedAt int64 `json:"createdAt"` // Unix ms
	UpdatedAt int64 `json:"updatedAt"`
}

// Variable 变量项（环境/集合/全局共用）
type Variable struct {
	Key     string `json:"key"`
	Value   string `json:"value"`
	Type    string `json:"type"` // default | secret
	Enabled bool   `json:"enabled"`
}

// Workspace 工作区
type Workspace struct {
	Id        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"` // local | team
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

// Environment 环境
type Environment struct {
	Id          string     `json:"id"`
	WorkspaceId string     `json:"workspaceId"`
	Name        string     `json:"name"`
	Variables   []Variable `json:"variables"`
	IsActive    bool       `json:"isActive"`
	CreatedAt   int64      `json:"createdAt"`
	UpdatedAt   int64      `json:"updatedAt"`
}

// HistoryQuery 历史列表查询参数
type HistoryQuery struct {
	Search string `json:"search,omitempty"` // 按 url/method 模糊过滤
	Limit  int    `json:"limit,omitempty"`  // 0 = 默认 50，最大 100
	Cursor string `json:"cursor,omitempty"` // opaque cursor；优先于 Offset
	Offset int    `json:"offset,omitempty"` // 兼容调用；新代码使用 Cursor
}

// HistorySummary 是列表专用投影，不携带 request snapshot、headers 或 body。
type HistorySummary struct {
	Id          string `json:"id"`
	WorkspaceId string `json:"workspaceId"`
	Method      string `json:"method"`
	Url         string `json:"url"`
	Status      int    `json:"status"`
	DurationMs  int64  `json:"durationMs"`
	SizeBytes   int64  `json:"sizeBytes"`
	HasBody     bool   `json:"hasBody"`
	CreatedAt   int64  `json:"createdAt"`
}

// HistoryPage 是稳定游标分页结果。
type HistoryPage struct {
	Items      []HistorySummary `json:"items"`
	NextCursor string           `json:"nextCursor,omitempty"`
	HasMore    bool             `json:"hasMore"`
}

// HistoryDetail 是单条历史的 replay/detail 投影。
type HistoryDetail struct {
	Id          string       `json:"id"`
	WorkspaceId string       `json:"workspaceId"`
	RequestSnap HttpRequest  `json:"requestSnap"` // 实际发送快照，凭证已不可逆脱敏
	Status      int          `json:"status"`
	DurationMs  int64        `json:"durationMs"`
	SizeBytes   int64        `json:"sizeBytes"`
	Timing      Timing       `json:"timing"`
	RespHeaders []KV         `json:"respHeaders"`
	BodyRef     string       `json:"bodyRef,omitempty"`
	BodyInline  string       `json:"bodyInline,omitempty"`
	TestResults []TestResult `json:"testResults,omitempty"`
	CreatedAt   int64        `json:"createdAt"`
}

// HistoryItem keeps internal callers source-compatible; public APIs use Summary/Detail explicitly.
type HistoryItem = HistoryDetail

// Example 请求的示例响应（"保存为示例"落点，Mock Server 数据源）
type Example struct {
	Id          string       `json:"id"`
	NodeId      string       `json:"nodeId"` // 所属请求节点
	Name        string       `json:"name"`
	RequestSnap *HttpRequest `json:"requestSnap,omitempty"` // 触发该示例的请求快照
	Status      int          `json:"status"`
	Headers     []KV         `json:"headers"`
	Body        string       `json:"body,omitempty"`
	CreatedAt   int64        `json:"createdAt"`
	UpdatedAt   int64        `json:"updatedAt"`
}
