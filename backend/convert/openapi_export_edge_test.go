package convert

import (
	"encoding/json"
	"strings"
	"testing"

	"apirequest/backend/model"
)

// 验证 collection-level Auth 会落到每个请求的 security/securitySchemes
func TestOpenAPIExportCollectionAuth(t *testing.T) {
	col := model.Node{
		Id:   "c1",
		Kind: "collection",
		Name: "WithAuth",
		Auth: &model.Auth{Type: "bearer", Params: map[string]string{"token": "tok"}},
	}
	children := []model.Node{
		{Id: "r1", Kind: "request", Name: "Ping", ParentId: "c1",
			Request: &model.HttpRequest{
				Method: "GET", Url: "https://x.io/ping",
				// request-level Auth 是 inherit，应从 collection 继承
				Auth:     model.Auth{Type: "inherit"},
				Settings: model.DefaultSettings(),
			}},
	}
	out, err := Export("openapi", col, children)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	var doc map[string]any
	json.Unmarshal([]byte(out), &doc)

	comp := doc["components"].(map[string]any)
	schemes := comp["securitySchemes"].(map[string]any)
	if _, ok := schemes["bearerAuth"]; !ok {
		t.Errorf("collection-level bearerAuth 未导出: %v", schemes)
	}

	paths := doc["paths"].(map[string]any)
	item := paths["/ping"].(map[string]any)
	getOp := item["get"].(map[string]any)
	sec := getOp["security"].([]any)
	if _, ok := sec[0].(map[string]any)["bearerAuth"]; !ok {
		t.Errorf("operation 未继承 collection Auth: %v", sec)
	}
}

func TestOpenAPIExportRequestNoAuthStopsInheritance(t *testing.T) {
	col := model.Node{
		Id: "c1", Kind: "collection",
		Auth: &model.Auth{Type: "bearer", Params: map[string]string{"token": "secret"}},
	}
	children := []model.Node{{
		Id: "r1", Kind: "request", Name: "Public", ParentId: "c1",
		Request: &model.HttpRequest{
			Method: "GET", Url: "https://x.io/public",
			Auth: model.Auth{Type: "none"}, Settings: model.DefaultSettings(),
		},
	}}
	out, err := Export("openapi", col, children)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	operation := doc["paths"].(map[string]any)["/public"].(map[string]any)["get"].(map[string]any)
	if _, ok := operation["security"]; ok {
		t.Fatalf("request-level none inherited collection auth: %v", operation["security"])
	}
}

// 验证重复 path+method 不会伪造 path，而是保存在 vendor extension 中。
func TestOpenAPIExportDuplicatePathMethod(t *testing.T) {
	col := model.Node{Id: "c1", Kind: "collection", Name: "Dup"}
	children := []model.Node{
		{Id: "r1", Kind: "request", Name: "First", ParentId: "c1", SortOrder: 1,
			Request: &model.HttpRequest{Method: "GET", Url: "https://x.io/users", Settings: model.DefaultSettings()}},
		{Id: "r2", Kind: "request", Name: "Second", ParentId: "c1", SortOrder: 2,
			Request: &model.HttpRequest{Method: "GET", Url: "https://x.io/users", Settings: model.DefaultSettings()}},
	}
	out, err := Export("openapi", col, children)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	var doc map[string]any
	json.Unmarshal([]byte(out), &doc)
	paths := doc["paths"].(map[string]any)
	item, ok := paths["/users"].(map[string]any)
	if !ok || item["get"] == nil {
		t.Errorf("主 path /users 缺失 GET: %v", paths)
	}
	if _, exists := paths["/users__dup_2"]; exists {
		t.Errorf("不应伪造重复 path: %v", paths)
	}
	alternates := item["x-apirequest-alternates"].([]any)
	if len(alternates) != 1 || alternates[0].(map[string]any)["method"] != "GET" {
		t.Fatalf("重复 operation 扩展错误: %v", alternates)
	}
	op2 := alternates[0].(map[string]any)["operation"].(map[string]any)
	if op2["summary"] != "Second" {
		t.Errorf("第二个 op summary 丢失: %v", op2["summary"])
	}
}

// 验证自定义 HTTP 方法不伪装成 GET，而是保存在 vendor extension 中。
func TestOpenAPIExportCustomMethod(t *testing.T) {
	col := model.Node{Id: "c1", Kind: "collection"}
	children := []model.Node{
		{Id: "r1", Kind: "request", Name: "Ping", ParentId: "c1",
			Request: &model.HttpRequest{Method: "GET", Url: "https://x.io/ping", Settings: model.DefaultSettings()}},
		{Id: "r2", Kind: "request", Name: "MKCOL", ParentId: "c1",
			Request: &model.HttpRequest{Method: "MKCOL", Url: "https://x.io/ping", Settings: model.DefaultSettings()}},
	}
	out, _ := Export("openapi", col, children)
	var doc map[string]any
	json.Unmarshal([]byte(out), &doc)
	paths := doc["paths"].(map[string]any)
	item := paths["/ping"].(map[string]any)
	alternates := item["x-apirequest-alternates"].([]any)
	if len(alternates) != 1 {
		t.Fatalf("自定义方法扩展缺失: %v", item)
	}
	alt := alternates[0].(map[string]any)
	if alt["method"] != "MKCOL" {
		t.Errorf("custom method = %v", alt["method"])
	}
	op2 := alt["operation"].(map[string]any)
	if op2["summary"] != "MKCOL" {
		t.Errorf("summary = %v", op2["summary"])
	}
}

func TestOpenAPIExportTraceMethod(t *testing.T) {
	col := model.Node{Id: "c1", Kind: "collection"}
	children := []model.Node{{
		Id: "r1", Kind: "request", Name: "Trace", ParentId: "c1",
		Request: &model.HttpRequest{Method: "TRACE", Url: "https://x.io/ping", Settings: model.DefaultSettings()},
	}}
	out, err := Export("openapi", col, children)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	item := doc["paths"].(map[string]any)["/ping"].(map[string]any)
	if item["trace"] == nil {
		t.Fatalf("TRACE operation missing: %v", item)
	}
}

func TestOpenAPIExportVariableURLQuery(t *testing.T) {
	col := model.Node{Id: "c1", Kind: "collection"}
	children := []model.Node{{
		Id: "r1", Kind: "request", Name: "List", ParentId: "c1",
		Request: &model.HttpRequest{Method: "GET", Url: "{{base}}/users?page=2", Settings: model.DefaultSettings()},
	}}
	out, err := Export("openapi", col, children)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, `"/users?page=2"`) {
		t.Fatalf("query leaked into OpenAPI path:\n%s", out)
	}
	if !strings.Contains(out, `"/users"`) || !strings.Contains(out, `"example": "2"`) {
		t.Fatalf("variable URL query not exported as parameter:\n%s", out)
	}
}
