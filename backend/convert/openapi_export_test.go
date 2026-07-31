package convert

import (
	"encoding/json"
	"strings"
	"testing"

	"apirequest/backend/model"
)

func TestOpenAPIExportRequest(t *testing.T) {
	col := model.Node{Id: "c1", Kind: "collection", Name: "Demo"}
	children := []model.Node{
		{Id: "f1", Kind: "folder", Name: "Users", ParentId: "c1", SortOrder: 1},
		{Id: "r1", Kind: "request", Name: "Get User", ParentId: "f1", SortOrder: 10,
			Request: &model.HttpRequest{
				Method:   "GET",
				Url:      "https://api.demo.io/users/1?full=true",
				Params:   []model.KV{{Key: "full", Value: "true", Enabled: true}},
				Headers:  []model.KV{{Key: "Accept", Value: "application/json", Enabled: true}},
				Auth:     model.Auth{Type: "bearer", Params: map[string]string{"token": "tok"}},
				Settings: model.DefaultSettings(),
			}},
		{Id: "r2", Kind: "request", Name: "Create User", ParentId: "f1", SortOrder: 20,
			Request: &model.HttpRequest{
				Method:   "POST",
				Url:      "https://api.demo.io/users",
				Headers:  []model.KV{{Key: "Content-Type", Value: "application/json", Enabled: true}},
				Body:     model.Body{Kind: "raw", Language: "json", Text: `{"name":"x"}`},
				Settings: model.DefaultSettings(),
			}},
	}
	out, err := Export("openapi", col, children)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if doc["openapi"] != "3.0.3" {
		t.Errorf("openapi = %v", doc["openapi"])
	}
	info := doc["info"].(map[string]any)
	if info["title"] != "Demo" {
		t.Errorf("title = %v", info["title"])
	}
	paths := doc["paths"].(map[string]any)
	if _, ok := paths["/users/1"]; !ok {
		t.Errorf("missing path /users/1: %v", paths)
	}
	if _, ok := paths["/users"]; !ok {
		t.Errorf("missing path /users: %v", paths)
	}
	// servers 应取 api.demo.io origin
	servers := doc["servers"].([]any)
	first := servers[0].(map[string]any)
	if first["url"] != "https://api.demo.io" {
		t.Errorf("server = %v", first["url"])
	}
	// tag Users 应被记录
	tags := doc["tags"].([]any)
	tag0 := tags[0].(map[string]any)
	if tag0["name"] != "Users" {
		t.Errorf("tag = %v", tag0["name"])
	}
	// components.securitySchemes.bearerAuth 应存在
	comp := doc["components"].(map[string]any)
	schemes := comp["securitySchemes"].(map[string]any)
	if _, ok := schemes["bearerAuth"]; !ok {
		t.Errorf("missing bearerAuth scheme: %v", schemes)
	}
	// operation 应带 security 引用
	item := paths["/users/1"].(map[string]any)
	getOp := item["get"].(map[string]any)
	parameters := getOp["parameters"].([]any)
	if len(parameters) != 2 {
		t.Fatalf("parameters = %v, want query + header", parameters)
	}
	query := parameters[0].(map[string]any)
	if query["name"] != "full" || query["in"] != "query" || query["example"] != "true" {
		t.Errorf("URL query 未正确导出: %v", query)
	}
	sec := getOp["security"].([]any)
	if _, ok := sec[0].(map[string]any)["bearerAuth"]; !ok {
		t.Errorf("operation security = %v", sec)
	}
	// raw json body 应映射到 application/json
	postItem := paths["/users"].(map[string]any)
	postOp := postItem["post"].(map[string]any)
	body := postOp["requestBody"].(map[string]any)
	content := body["content"].(map[string]any)
	if _, ok := content["application/json"]; !ok {
		t.Errorf("missing application/json body: %v", content)
	}
}

func TestOpenAPIExportUnsupportedFormat(t *testing.T) {
	// 占位：确保未注册格式仍按 convert.Export 返回错误
	_, err := Export("no-such-format", model.Node{}, nil)
	if err == nil {
		t.Error("expected error for unknown export format")
	}
}

// 验证可被 Postman 反向导入不崩溃（仅做结构可解析的烟雾测试）
func TestOpenAPIExportIsParseable(t *testing.T) {
	col := model.Node{Id: "c1", Kind: "collection", Name: "Solo"}
	children := []model.Node{
		{Id: "r1", Kind: "request", Name: "Ping", ParentId: "c1",
			Request: &model.HttpRequest{Method: "GET", Url: "https://x.io/ping", Settings: model.DefaultSettings()}},
	}
	out, _ := Export("openapi", col, children)
	if !strings.Contains(out, "\"openapi\": \"3.0.3\"") {
		t.Errorf("unexpected output:\n%s", out)
	}
}
