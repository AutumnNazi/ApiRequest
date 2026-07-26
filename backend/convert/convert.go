// Package convert 实现导入导出转换器（docs/interop.md）。
// 统一走 外部格式 → 内部模型(IR) → 外部格式；IR 即 model 包共享类型。
package convert

import (
	"fmt"

	"apirequest/backend/model"
)

// ImportResult 导入产物：一棵待落库的集合树（前端预览确认后再入库）
type ImportResult struct {
	Collection model.Node   `json:"collection"` // 根（kind=collection）
	Children   []model.Node `json:"children"`   // 全部后代（parentId 已串好，id 为占位）
	Warnings   []string     `json:"warnings"`   // 转换中丢失/降级的信息
}

// Importer 导入器接口
type Importer interface {
	Format() string
	// Detect 粗判 payload 是否本格式（多格式自动识别用）
	Detect(payload string) bool
	Import(payload string) (*ImportResult, error)
}

// Exporter 导出器接口
type Exporter interface {
	Format() string
	// Export 把集合树序列化为目标格式文本
	Export(collection model.Node, children []model.Node) (string, error)
}

var importers = map[string]Importer{}
var exporters = map[string]Exporter{}

// RegisterImporter / RegisterExporter 注册（init 时调用）
func RegisterImporter(i Importer) { importers[i.Format()] = i }
func RegisterExporter(e Exporter) { exporters[e.Format()] = e }

// Import 按格式导入；format=auto 时自动识别
func Import(format, payload string) (*ImportResult, error) {
	if format == "auto" || format == "" {
		for _, i := range importers {
			if i.Detect(payload) {
				return i.Import(payload)
			}
		}
		return nil, model.NewError(model.KindImport, "unrecognized import format")
	}
	i, ok := importers[format]
	if !ok {
		return nil, &model.AppError{Kind: model.KindImport, Format: format,
			Detail: fmt.Sprintf("unsupported import format: %s", format)}
	}
	return i.Import(payload)
}

// Export 按格式导出
func Export(format string, collection model.Node, children []model.Node) (string, error) {
	e, ok := exporters[format]
	if !ok {
		return "", &model.AppError{Kind: model.KindImport, Format: format,
			Detail: fmt.Sprintf("unsupported export format: %s", format)}
	}
	return e.Export(collection, children)
}
