package convert

import (
	"encoding/json"
	"strings"
	"testing"

	"apirequest/backend/model"
)

// 验证 URL 已有 query 而 Params 又有同名 key 时，不会重复追加
func TestCurlExportQueryDedup(t *testing.T) {
	col := model.Node{Id: "c1", Kind: "collection"}
	children := []model.Node{
		{Id: "r1", Kind: "request", Name: "Get", ParentId: "c1",
			Request: &model.HttpRequest{
				Method: "GET",
				Url:    "https://x.io/users?id=1",
				Params: []model.KV{
					{Key: "id", Value: "1", Enabled: true}, // 重复，应跳过
					{Key: "verbose", Value: "true", Enabled: true},
				},
				Settings: model.DefaultSettings(),
			}},
	}
	out, err := Export("curl", col, children)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Items []struct {
			Script string `json:"script"`
		} `json:"items"`
	}
	json.Unmarshal([]byte(out), &doc)
	script := doc.Items[0].Script
	// id 应只出现一次
	if strings.Count(script, "id=") != 1 {
		t.Errorf("query id 重复:\n%s", script)
	}
	if !strings.Contains(script, "verbose=") {
		t.Errorf("verbose 应被追加:\n%s", script)
	}
}

// 验证 collection-level Auth 继承到 cURL 脚本与 item.Auth
func TestCurlExportCollectionAuth(t *testing.T) {
	col := model.Node{
		Id:   "c1",
		Kind: "collection",
		Auth: &model.Auth{Type: "bearer", Params: map[string]string{"token": "tok"}},
	}
	children := []model.Node{
		{Id: "r1", Kind: "request", Name: "Ping", ParentId: "c1",
			Request: &model.HttpRequest{
				Method: "GET", Url: "https://x.io/ping",
				Auth:     model.Auth{Type: "inherit"},
				Settings: model.DefaultSettings(),
			}},
	}
	out, _ := Export("curl", col, children)
	var doc struct {
		Items []struct {
			Auth *struct {
				Type   string            `json:"type"`
				Params map[string]string `json:"params"`
			} `json:"auth"`
			Script string `json:"script"`
		} `json:"items"`
	}
	json.Unmarshal([]byte(out), &doc)
	if doc.Items[0].Auth == nil || doc.Items[0].Auth.Type != "bearer" {
		t.Errorf("item.Auth 未继承 collection Auth: %+v", doc.Items[0].Auth)
	}
	if !strings.Contains(doc.Items[0].Script, "Authorization: Bearer tok") {
		t.Errorf("shell 缺 bearer: %s", doc.Items[0].Script)
	}
}

func TestCurlExportRequestNoAuthStopsInheritance(t *testing.T) {
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
	out, err := Export("curl", col, children)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "secret") || strings.Contains(out, "Authorization") {
		t.Fatalf("request-level none inherited collection auth:\n%s", out)
	}
}

func TestCurlExportGraphQLVariablesRemainJSON(t *testing.T) {
	col := model.Node{Id: "c1", Kind: "collection"}
	children := []model.Node{{
		Id: "r1", Kind: "request", Name: "GraphQL", ParentId: "c1",
		Request: &model.HttpRequest{
			Method: "POST", Url: "https://x.io/graphql",
			Body:     model.Body{Kind: "graphql", Query: "query($id: ID!) { user(id: $id) { name } }", Variables: "{\"id\":1}"},
			Settings: model.DefaultSettings(),
		},
	}}
	out, err := Export("curl", col, children)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatal(err)
	}
	items, ok := doc["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v", doc["items"])
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("item = %#v", items[0])
	}
	script, ok := item["script"].(string)
	if !ok {
		t.Fatalf("script = %#v", item["script"])
	}
	if strings.Contains(script, "\"variables\":\"{") {
		t.Fatalf("GraphQL variables were encoded as a JSON string:\n%s", script)
	}
	if !strings.Contains(script, "\"variables\":{\"id\":1}") {
		t.Fatalf("GraphQL variables object missing:\n%s", script)
	}
}

func TestCurlExportApiKeyQuery(t *testing.T) {
	col := model.Node{Id: "c1", Kind: "collection"}
	children := []model.Node{{
		Id: "r1", Kind: "request", Name: "Key", ParentId: "c1",
		Request: &model.HttpRequest{
			Method: "GET", Url: "https://x.io/items",
			Auth: model.Auth{Type: "apikey", Params: map[string]string{
				"in": "query", "key": "api_key", "value": "secret",
			}},
			Settings: model.DefaultSettings(),
		},
	}}
	out, err := Export("curl", col, children)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "https://x.io/items?api_key=secret") {
		t.Fatalf("query API key missing from URL:\n%s", out)
	}
	if strings.Contains(out, "api_key: secret") {
		t.Fatalf("query API key incorrectly exported as header:\n%s", out)
	}
}
