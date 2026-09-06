// Postman 环境文件导入（.postman_environment JSON）。
// 环境文件没有集合语义：产出 = 空占位集合 + SuggestedEnvironments[0]，
// ImportCommit 会把建议环境落库（不激活）。检测标记 `_postman_variable_scope`，
// 与集合文件（schema.getpostman.com）互不冲突。
package convert

import (
	"encoding/json"
	"fmt"
	"strings"

	"apirequest/backend/model"
)

type postmanEnvImporter struct{}

func (postmanEnvImporter) Format() string { return "postman-env" }

func (postmanEnvImporter) Detect(payload string) bool {
	// 只认 environment scope；collection scope 属于集合导入器
	return strings.Contains(payload, `"_postman_variable_scope"`) &&
		strings.Contains(payload, `"environment"`)
}

type postmanEnvFile struct {
	Name   string `json:"name"`
	Values []struct {
		Key     string `json:"key"`
		Value   string `json:"value"`
		Enabled *bool  `json:"enabled"`
	} `json:"values"`
}

func (postmanEnvImporter) Import(payload string) (*ImportResult, error) {
	var f postmanEnvFile
	if err := json.Unmarshal([]byte(payload), &f); err != nil {
		return nil, &model.AppError{Kind: model.KindImport, Format: "postman-env",
			Detail: "invalid Postman environment JSON: " + err.Error()}
	}
	if f.Name == "" {
		return nil, &model.AppError{Kind: model.KindImport, Format: "postman-env",
			Detail: "environment file has no name"}
	}
	var vars []model.Variable
	for _, v := range f.Values {
		if v.Key == "" {
			continue
		}
		// Postman 旧导出缺 enabled 字段视为启用；显式 false 才剔除
		enabled := v.Enabled == nil || *v.Enabled
		if !enabled {
			continue
		}
		vars = append(vars, model.Variable{Key: v.Key, Value: v.Value, Type: "default", Enabled: true})
	}
	if len(vars) == 0 {
		return nil, &model.AppError{Kind: model.KindImport, Format: "postman-env",
			Detail: "environment file has no enabled values"}
	}
	return &ImportResult{
		Collection: model.Node{
			Id: "import-root", Kind: "collection",
			Name: fmt.Sprintf("Postman 环境 %s（仅导入变量）", f.Name),
		},
		Children: []model.Node{},
		SuggestedEnvironments: []SuggestedEnvironment{
			{Name: f.Name, Variables: vars},
		},
		Warnings: []string{"环境文件不包含请求；确认导入后变量会以环境「" + f.Name + "」落地，占位集合可删除"},
	}, nil
}

func init() { RegisterImporter(postmanEnvImporter{}) }
