package model

// RunnerRunQuery Runner 运行历史查询参数（docs/roadmap.md：Runner 报告持久化）
type RunnerRunQuery struct {
	CollectionId string `json:"collectionId,omitempty"` // 按集合过滤
	Limit        int    `json:"limit,omitempty"`        // 0 = 默认 20
	Cursor       string `json:"cursor,omitempty"`       // opaque cursor，形态与 HistoryQuery 一致
}

// RunnerRunSummary 是列表专用投影，不携带 results 明细（避免扫描大 JSON）。
type RunnerRunSummary struct {
	RunId        string `json:"runId"`
	CollectionId string `json:"collectionId"`
	Total        int    `json:"total"`
	Passed       int    `json:"passed"`
	Failed       int    `json:"failed"`
	Skipped      int    `json:"skipped"`
	DurationMs   int64  `json:"durationMs"`
	Canceled     bool   `json:"canceled"`
	CreatedAt    int64  `json:"createdAt"`
}

// RunnerRunPage 是稳定游标分页结果。
type RunnerRunPage struct {
	Items      []RunnerRunSummary `json:"items"`
	NextCursor string             `json:"nextCursor,omitempty"`
	HasMore    bool               `json:"hasMore"`
}

// StorageStats 存储体检快照（设置页"存储"分区展示）
type StorageStats struct {
	Workspaces   int64 `json:"workspaces"`
	Nodes        int64 `json:"nodes"`
	Examples     int64 `json:"examples"`
	Environments int64 `json:"environments"`
	History      int64 `json:"history"`
	RunnerRuns   int64 `json:"runnerRuns"`
	BlobFiles    int64 `json:"blobFiles"`
	BlobBytes    int64 `json:"blobBytes"`
	DbBytes      int64 `json:"dbBytes"`
	WalBytes     int64 `json:"walBytes"`
}
