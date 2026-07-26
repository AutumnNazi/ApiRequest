package model

import "encoding/json"

// ErrorKind 错误分类，见 docs/ops.md §2
type ErrorKind string

const (
	KindNetwork    ErrorKind = "network"
	KindTls        ErrorKind = "tls"
	KindScript     ErrorKind = "script"
	KindStorage    ErrorKind = "storage"
	KindImport     ErrorKind = "import"
	KindValidation ErrorKind = "validation"
)

// AppError 结构化错误。跨 Wails 边界时 JSON 序列化为 error 文本，
// 前端 ipc wrapper 反解析（docs/api-contract.md §2）。
type AppError struct {
	Kind   ErrorKind `json:"kind"`
	Detail string    `json:"detail"`
	Phase  string    `json:"phase,omitempty"`  // 脚本错误阶段 pre/test
	Line   *uint32   `json:"line,omitempty"`   // 脚本错误行号
	Format string    `json:"format,omitempty"` // 导入错误格式
}

// Error 实现 error 接口：输出 JSON，保证前端拿到的 rejection 文本可反解析
func (e *AppError) Error() string {
	b, err := json.Marshal(e)
	if err != nil {
		return `{"kind":"` + string(e.Kind) + `","detail":"marshal failed"}`
	}
	return string(b)
}

// NewError 便捷构造
func NewError(kind ErrorKind, detail string) *AppError {
	return &AppError{Kind: kind, Detail: detail}
}

// WrapError 把任意 error 归一化为 AppError；已是 AppError 则原样返回
func WrapError(kind ErrorKind, err error) *AppError {
	if err == nil {
		return nil
	}
	if ae, ok := err.(*AppError); ok {
		return ae
	}
	return &AppError{Kind: kind, Detail: err.Error()}
}
