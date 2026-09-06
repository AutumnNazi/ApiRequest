// Package runner 实现 Collection Runner 执行引擎（docs/advanced.md §2）。
// 报告仅存内存，由用户导出（docs/data-model.md：明确不持久化）。
package runner

import (
	"encoding/csv"
	"encoding/json"
	"sort"
	"strings"

	"apirequest/backend/model"
)

// Options 运行选项
type Options struct {
	DataFile     string            `json:"dataFile,omitempty"`   // CSV 或 JSON 文本（前端读文件后传入）
	DataFormat   string            `json:"dataFormat,omitempty"` // csv | json | 空=无数据文件
	StopOnError  bool              `json:"stopOnError"`
	Iterations   int               `json:"iterations,omitempty"`   // 无数据文件时的轮数，默认 1
	EnvId        string            `json:"envId,omitempty"`        // 指定环境（CLI --env）；空 = 工作区激活环境
	EnvOverrides map[string]string `json:"envOverrides,omitempty"` // CLI --env-file 注入的变量（优先级高于环境变量，低于数据行）
	Concurrency  int               `json:"concurrency,omitempty"`  // 并发工作协程数；<=1 串行（压测场景）
	// DelayMs 相邻请求间的等待毫秒（think-time）：串行=任务之间；并发=每个 worker 的任务之间。
	// 压测速率控制（与迭代数配合逼近目标 RPS）；0 = 不等待
	DelayMs int `json:"delayMs,omitempty"`
}

// MergeOverrides 合并 CLI 环境文件变量与数据行：数据行优先（同 key 覆盖）。
// 返回新 map，不修改入参。
func MergeOverrides(envFileVars, row map[string]string) map[string]string {
	if len(envFileVars) == 0 && len(row) == 0 {
		return map[string]string{}
	}
	merged := make(map[string]string, len(envFileVars)+len(row))
	for k, v := range envFileVars {
		merged[k] = v
	}
	for k, v := range row {
		merged[k] = v
	}
	return merged
}

// RequestResult 单请求执行明细
type RequestResult struct {
	Iteration   int                `json:"iteration"`
	RequestName string             `json:"requestName"`
	NodeId      string             `json:"nodeId"`
	Status      int                `json:"status"`
	DurationMs  int64              `json:"durationMs"`
	Failed      bool               `json:"failed"` // 网络错误或有断言失败
	Error       string             `json:"error,omitempty"`
	TestResults []model.TestResult `json:"testResults"`
}

// Report 运行汇总报告
type Report struct {
	RunId      string          `json:"runId"`
	Total      int             `json:"total"`
	Passed     int             `json:"passed"`
	Failed     int             `json:"failed"`
	Skipped    int             `json:"skipped"`
	DurationMs int64           `json:"durationMs"`
	Results    []RequestResult `json:"results"`
	Canceled   bool            `json:"canceled"`
	CreatedAt  int64           `json:"createdAt,omitempty"` // 落库时间戳（内存报告为 0，导出时不出现）
}

// ParseDataFile 解析数据文件为迭代行（docs/advanced.md：每行注入 data 作用域）
func ParseDataFile(content, format string) ([]map[string]string, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, nil
	}
	switch format {
	case "json":
		var rows []map[string]any
		if err := json.Unmarshal([]byte(content), &rows); err != nil {
			return nil, model.NewError(model.KindValidation, "invalid JSON data file: "+err.Error())
		}
		out := make([]map[string]string, len(rows))
		for i, r := range rows {
			out[i] = map[string]string{}
			for k, v := range r {
				out[i][k] = toStr(v)
			}
		}
		return out, nil
	case "csv":
		r := csv.NewReader(strings.NewReader(content))
		records, err := r.ReadAll()
		if err != nil {
			return nil, model.NewError(model.KindValidation, "invalid CSV data file: "+err.Error())
		}
		if len(records) < 2 {
			return nil, model.NewError(model.KindValidation, "CSV needs a header row and at least one data row")
		}
		header := records[0]
		out := make([]map[string]string, 0, len(records)-1)
		for _, rec := range records[1:] {
			row := map[string]string{}
			for i, h := range header {
				if i < len(rec) {
					row[strings.TrimSpace(h)] = rec[i]
				}
			}
			out = append(out, row)
		}
		return out, nil
	default:
		return nil, model.NewError(model.KindValidation, "unknown data format: "+format)
	}
}

func toStr(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

// FlattenOrdered 按树的显示顺序展开集合下的全部请求节点（docs/advanced.md：执行顺序）
func FlattenOrdered(collectionId string, nodes []model.Node) []model.Node {
	byParent := map[string][]model.Node{}
	for _, n := range nodes {
		byParent[n.ParentId] = append(byParent[n.ParentId], n)
	}
	for _, list := range byParent {
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].SortOrder != list[j].SortOrder {
				return list[i].SortOrder < list[j].SortOrder
			}
			return list[i].CreatedAt < list[j].CreatedAt
		})
	}
	var out []model.Node
	var walk func(parentId string)
	walk = func(parentId string) {
		for _, n := range byParent[parentId] {
			if n.Kind == "request" && n.Request != nil {
				out = append(out, n)
			}
			walk(n.Id)
		}
	}
	walk(collectionId)
	return out
}
