package convert

import (
	"encoding/json"
	"testing"

	"apirequest/backend/model"
)

func TestSwagger2ExportBasic(t *testing.T) {
	col := model.Node{Id: "c1", Kind: "collection", Name: "SwaggerDemo"}
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
	out, err := Export("swagger2", col, children)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if doc["swagger"] != "2.0" {
		t.Errorf("swagger = %v, want 2.0", doc["swagger"])
	}
	info := doc["info"].(map[string]any)
	if info["title"] != "SwaggerDemo" {
		t.Errorf("title = %v", info["title"])
	}
	if doc["host"] != "api.demo.io" {
		t.Errorf("host = %v, want api.demo.io", doc["host"])
	}
	schemes := doc["schemes"].([]any)
	if len(schemes) == 0 || schemes[0] != "https" {
		t.Errorf("schemes = %v, want [https]", schemes)
	}
	paths := doc["paths"].(map[string]any)
	if _, ok := paths["/users/1"]; !ok {
		t.Errorf("missing path /users/1: %v", paths)
	}
	if _, ok := paths["/users"]; !ok {
		t.Errorf("missing path /users: %v", paths)
	}

	// tags Users 应被记录
	tags := doc["tags"].([]any)
	tag0 := tags[0].(map[string]any)
	if tag0["name"] != "Users" {
		t.Errorf("tag = %v", tag0["name"])
	}

	// securityDefinitions.bearerAuth 应存在
	secDefs := doc["securityDefinitions"].(map[string]any)
	bearer, ok := secDefs["bearerAuth"].(map[string]any)
	if !ok {
		t.Errorf("missing bearerAuth scheme: %v", secDefs)
	} else if bearer["type"] != "apiKey" || bearer["in"] != "header" || bearer["name"] != "Authorization" {
		t.Errorf("bearer scheme = %v", bearer)
	}

	// GET operation 应有 query + header 参数
	item := paths["/users/1"].(map[string]any)
	getOp := item["get"].(map[string]any)
	parameters := getOp["parameters"].([]any)
	if len(parameters) != 2 {
		t.Fatalf("parameters = %v, want query + header", parameters)
	}
	query := parameters[0].(map[string]any)
	if query["name"] != "full" || query["in"] != "query" {
		t.Errorf("URL query 未正确导出: %v", query)
	}

	// POST operation 应有 body 参数 + consumes
	postItem := paths["/users"].(map[string]any)
	postOp := postItem["post"].(map[string]any)
	consumes := postOp["consumes"].([]any)
	if len(consumes) == 0 || consumes[0] != "application/json" {
		t.Errorf("consumes = %v, want [application/json]", consumes)
	}
	postParams := postOp["parameters"].([]any)
	// 最后一个应是 body 参数
	bodyParam := postParams[len(postParams)-1].(map[string]any)
	if bodyParam["in"] != "body" || bodyParam["name"] != "body" {
		t.Errorf("body param = %v", bodyParam)
	}
}

func TestSwagger2ExportFormat(t *testing.T) {
	e := swagger2Exporter{}
	if e.Format() != "swagger2" {
		t.Error("format mismatch")
	}
}

func TestSwagger2ExportRelativeUrl(t *testing.T) {
	col := model.Node{Id: "c1", Kind: "collection", Name: "Rel"}
	children := []model.Node{
		{Id: "r1", Kind: "request", Name: "Ping", ParentId: "c1",
			Request: &model.HttpRequest{Method: "GET", Url: "/api/ping", Settings: model.DefaultSettings()}},
	}
	out, err := Export("swagger2", col, children)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	paths := doc["paths"].(map[string]any)
	if _, ok := paths["/api/ping"]; !ok {
		t.Errorf("missing relative path /api/ping: %v", paths)
	}
	// 相对 URL 不应输出 host
	if doc["host"] != nil {
		t.Errorf("host should be empty for relative url, got %v", doc["host"])
	}
}

func TestSwagger2ExportFormData(t *testing.T) {
	col := model.Node{Id: "c1", Kind: "collection", Name: "Form"}
	children := []model.Node{
		{Id: "r1", Kind: "request", Name: "Upload", ParentId: "c1",
			Request: &model.HttpRequest{
				Method: "POST",
				Url:    "https://api.demo.io/upload",
				Body: model.Body{Kind: "formdata", Items: []model.FormItem{
					{Key: "name", Type: "text", Value: "x", Enabled: true},
					{Key: "file", Type: "file", Enabled: true},
				}},
				Settings: model.DefaultSettings(),
			}},
	}
	out, err := Export("swagger2", col, children)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	paths := doc["paths"].(map[string]any)
	item := paths["/upload"].(map[string]any)
	postOp := item["post"].(map[string]any)
	consumes := postOp["consumes"].([]any)
	if len(consumes) == 0 || consumes[0] != "multipart/form-data" {
		t.Errorf("consumes = %v, want [multipart/form-data]", consumes)
	}
	parameters := postOp["parameters"].([]any)
	if len(parameters) != 2 {
		t.Fatalf("parameters = %v, want one formData parameter per field", parameters)
	}
	byName := map[string]map[string]any{}
	for _, raw := range parameters {
		parameter := raw.(map[string]any)
		byName[parameter["name"].(string)] = parameter
		if parameter["in"] != "formData" || parameter["schema"] != nil {
			t.Fatalf("invalid Swagger formData parameter: %v", parameter)
		}
	}
	if byName["name"]["type"] != "string" || byName["file"]["type"] != "file" {
		t.Fatalf("formData types = %v", byName)
	}
}

func TestSwagger2ExportRejectsDuplicateOperation(t *testing.T) {
	col := model.Node{Id: "c1", Kind: "collection", Name: "Duplicates"}
	children := []model.Node{
		{Id: "r1", ParentId: "c1", Kind: "request", Name: "one", Request: &model.HttpRequest{Method: "GET", Url: "https://api.example.test/items", Settings: model.DefaultSettings()}},
		{Id: "r2", ParentId: "c1", Kind: "request", Name: "two", Request: &model.HttpRequest{Method: "GET", Url: "https://api.example.test/items", Settings: model.DefaultSettings()}},
	}
	if _, err := Export("swagger2", col, children); err == nil {
		t.Fatal("duplicate path/method was silently overwritten")
	}
}

func TestSwagger2ExportRejectsUnsupportedMethod(t *testing.T) {
	col := model.Node{Id: "c1", Kind: "collection", Name: "Custom"}
	children := []model.Node{{
		Id: "r1", ParentId: "c1", Kind: "request", Name: "purge",
		Request: &model.HttpRequest{Method: "PURGE", Url: "https://api.example.test/cache", Settings: model.DefaultSettings()},
	}}
	if _, err := Export("swagger2", col, children); err == nil {
		t.Fatal("unsupported method was silently dropped")
	}
}

func TestSwagger2ExportRejectsMultipleHosts(t *testing.T) {
	col := model.Node{Id: "c1", Kind: "collection", Name: "Hosts"}
	children := []model.Node{
		{Id: "r1", ParentId: "c1", Kind: "request", Name: "one", Request: &model.HttpRequest{Method: "GET", Url: "https://one.example.test/items", Settings: model.DefaultSettings()}},
		{Id: "r2", ParentId: "c1", Kind: "request", Name: "two", Request: &model.HttpRequest{Method: "GET", Url: "https://two.example.test/users", Settings: model.DefaultSettings()}},
	}
	if _, err := Export("swagger2", col, children); err == nil {
		t.Fatal("multiple hosts were silently collapsed to the first host")
	}
}
