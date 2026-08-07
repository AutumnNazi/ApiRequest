package convert

import (
	"encoding/json"
	"strings"
	"testing"

	"apirequest/backend/model"
)

func TestOpenAPI31ExportVersion(t *testing.T) {
	col := model.Node{Id: "c1", Kind: "collection", Name: "Demo31"}
	children := []model.Node{
		{Id: "r1", Kind: "request", Name: "Get User", ParentId: "c1", SortOrder: 1,
			Request: &model.HttpRequest{
				Method:   "GET",
				Url:      "https://api.demo.io/users/1?full=true",
				Headers:  []model.KV{{Key: "Accept", Value: "application/json", Enabled: true}},
				Settings: model.DefaultSettings(),
			}},
	}
	out, err := Export("openapi3.1", col, children)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if doc["openapi"] != "3.1.0" {
		t.Errorf("openapi = %v, want 3.1.0", doc["openapi"])
	}
	if doc["jsonSchemaDialect"] != "https://json-schema.org/draft/2020-12/schema" {
		t.Errorf("jsonSchemaDialect = %v", doc["jsonSchemaDialect"])
	}
	info := doc["info"].(map[string]any)
	if info["title"] != "Demo31" {
		t.Errorf("title = %v", info["title"])
	}
	paths := doc["paths"].(map[string]any)
	if _, ok := paths["/users/1"]; !ok {
		t.Errorf("missing path /users/1: %v", paths)
	}
	servers := doc["servers"].([]any)
	first := servers[0].(map[string]any)
	if first["url"] != "https://api.demo.io" {
		t.Errorf("server = %v", first["url"])
	}
	if !strings.Contains(out, "\"openapi\": \"3.1.0\"") {
		t.Errorf("output should contain 3.1.0 version:\n%s", out)
	}
}

func TestOpenAPI31ExportFormat(t *testing.T) {
	e := openapi31Exporter{}
	if e.Format() != "openapi3.1" {
		t.Error("format mismatch")
	}
}
