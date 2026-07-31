package convert

import (
	"encoding/json"
	"strings"
	"testing"

	"apirequest/backend/model"
)

func TestCurlExportRequest(t *testing.T) {
	col := model.Node{Id: "c1", Kind: "collection", Name: "Demo"}
	children := []model.Node{
		{Id: "r1", Kind: "request", Name: "Get User", ParentId: "c1", SortOrder: 10,
			Request: &model.HttpRequest{
				Method:  "GET",
				Url:     "https://api.demo.io/users/1",
				Params:  []model.KV{{Key: "full", Value: "true", Enabled: true}},
				Headers: []model.KV{{Key: "Accept", Value: "application/json", Enabled: true}},
				Auth:    model.Auth{Type: "bearer", Params: map[string]string{"token": "tok"}},
				Settings: model.DefaultSettings(),
			}},
		{Id: "r2", Kind: "request", Name: "Create User", ParentId: "c1", SortOrder: 20,
			Request: &model.HttpRequest{
				Method:  "POST",
				Url:     "https://api.demo.io/users",
				Headers: []model.KV{{Key: "Content-Type", Value: "application/json", Enabled: true}},
				Body:    model.Body{Kind: "raw", Language: "json", Text: `{"name":"x"}`},
				Settings: model.DefaultSettings(),
			}},
	}
	out, err := Export("curl", col, children)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	var doc struct {
		Collection string `json:"collection"`
		Items      []struct {
			Name    string `json:"name"`
			Method  string `json:"method"`
			Url     string `json:"url"`
			Script  string `json:"script"`
		} `json:"items"`
		Shell string `json:"shell"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if doc.Collection != "Demo" {
		t.Errorf("collection = %v", doc.Collection)
	}
	if len(doc.Items) != 2 {
		t.Fatalf("items = %d", len(doc.Items))
	}
	if doc.Items[0].Name != "Get User" || doc.Items[0].Method != "GET" {
		t.Errorf("item0 = %+v", doc.Items[0])
	}

	// shell 里应有两条 curl
	if strings.Count(doc.Shell, "curl ") != 2 {
		t.Errorf("shell curl count = %d\n%s", strings.Count(doc.Shell, "curl "), doc.Shell)
	}
	// 集合名写入 shell 注释
	if !strings.Contains(doc.Shell, "Demo") {
		t.Errorf("shell missing collection name: %s", doc.Shell)
	}
	// bearer token 应被拼进 -H Authorization: Bearer tok
	if !strings.Contains(doc.Items[0].Script, "Authorization: Bearer tok") {
		t.Errorf("missing bearer auth: %s", doc.Items[0].Script)
	}
	// 第二个请求应带 -d JSON
	if !strings.Contains(doc.Items[1].Script, "-d '") {
		t.Errorf("missing -d body: %s", doc.Items[1].Script)
	}
}

func TestCurlExportSorted(t *testing.T) {
	// 验证按 SortOrder 排序：故意乱序构造，确认导出 items 顺序保持
	col := model.Node{Id: "c1", Kind: "collection"}
	children := []model.Node{
		{Id: "r2", Kind: "request", Name: "B", ParentId: "c1", SortOrder: 20,
			Request: &model.HttpRequest{Method: "GET", Url: "https://x.io/b", Settings: model.DefaultSettings()}},
		{Id: "r1", Kind: "request", Name: "A", ParentId: "c1", SortOrder: 10,
			Request: &model.HttpRequest{Method: "GET", Url: "https://x.io/a", Settings: model.DefaultSettings()}},
	}
	out, _ := Export("curl", col, children)
	var doc struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	json.Unmarshal([]byte(out), &doc)
	if len(doc.Items) != 2 || doc.Items[0].Name != "A" || doc.Items[1].Name != "B" {
		t.Errorf("items order = %+v", doc.Items)
	}
}

func TestCurlExportFormdata(t *testing.T) {
	col := model.Node{Id: "c1", Kind: "collection"}
	children := []model.Node{
		{Id: "r1", Kind: "request", Name: "Upload", ParentId: "c1",
			Request: &model.HttpRequest{
				Method: "POST", Url: "https://x.io/upload",
				Body: model.Body{Kind: "formdata", Items: []model.FormItem{
					{Key: "name", Value: "pic", Enabled: true},
					{Key: "file", Type: "file", Path: "/tmp/a.png", Enabled: true},
				}},
				Settings: model.DefaultSettings(),
			}},
	}
	out, _ := Export("curl", col, children)
	var doc struct {
		Items []struct {
			Script string `json:"script"`
		} `json:"items"`
	}
	json.Unmarshal([]byte(out), &doc)
	if !strings.Contains(doc.Items[0].Script, "-F 'name=pic'") {
		t.Errorf("missing name field: %s", doc.Items[0].Script)
	}
	if !strings.Contains(doc.Items[0].Script, "-F 'file=@/tmp/a.png'") {
		t.Errorf("missing file field: %s", doc.Items[0].Script)
	}
}
