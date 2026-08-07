package convert

import (
	"encoding/json"

	"apirequest/backend/model"
)

// OpenAPI 3.1.0 集合导出。
// 3.1 与 3.0 的主要差异：
//   - openapi 版本号 "3.1.0"
//   - jsonSchemaDialect 字段
//   - Schema 使用 JSON Schema 2020-12（nullable → type 数组）
// 复用 3.0.3 的路径/参数/安全逻辑，仅覆盖版本号。

type openapi31Exporter struct{}

func (openapi31Exporter) Format() string { return "openapi3.1" }

func (openapi31Exporter) Export(collection model.Node, children []model.Node) (string, error) {
	base, err := openapiExporter{}.Export(collection, children)
	if err != nil {
		return "", err
	}

	var doc oeDoc
	if err := json.Unmarshal([]byte(base), &doc); err != nil {
		return "", err
	}

	doc.Openapi = "3.1.0"
	doc.JsonSchemaDialect = "https://json-schema.org/draft/2020-12/schema"

	out, err := json.MarshalIndent(doc, "", "  ")
	return string(out), err
}

func init() {
	RegisterExporter(openapi31Exporter{})
}
