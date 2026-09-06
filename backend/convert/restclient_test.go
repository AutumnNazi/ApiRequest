package convert

import (
	"strings"
	"testing"

	"apirequest/backend/model"
)

func TestRestClientImporterDetect(t *testing.T) {
	i := restClientImporter{}
	if !i.Detect("### 登录\nGET https://x.test/api") {
		t.Fatal("detect: ### 分隔的 .http 应命中")
	}
	if !i.Detect("GET https://x.test/api\nAuthorization: Bearer x") {
		t.Fatal("detect: 方法行开头应命中")
	}
	if i.Detect(`{"info":{"name":"coll"}}`) {
		t.Fatal("postman JSON 不应命中")
	}
	if i.Detect("") {
		t.Fatal("空内容不应命中")
	}
}

func TestRestClientImporterMultiRequest(t *testing.T) {
	payload := strings.Join([]string{
		"# 集合注释",
		"### 登录",
		"POST https://api.example.com/login",
		"Content-Type: application/json",
		"Authorization: Bearer {{token}}",
		"",
		`{"user":"alice"}`,
		"",
		"### 列表（带 query）",
		"GET https://api.example.com/orders?page=2&size=20",
		"Accept: application/json",
		"",
		"### 仅 URL 行",
		"DELETE https://api.example.com/orders/7",
	}, "\n")

	res, err := restClientImporter{}.Import(payload)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Collection.Kind != "collection" {
		t.Fatalf("collection = %+v", res.Collection)
	}
	if len(res.Children) != 3 {
		t.Fatalf("children = %d, want 3", len(res.Children))
	}
	first := res.Children[0]
	if first.Name != "登录" || first.ParentId != res.Collection.Id || first.Request == nil {
		t.Fatalf("first = %+v", first)
	}
	if first.Request.Method != "POST" || first.Request.Url != "https://api.example.com/login" {
		t.Fatalf("first.request = %+v", first.Request)
	}
	if len(first.Request.Headers) != 2 {
		t.Fatalf("headers = %+v", first.Request.Headers)
	}
	if first.Request.Body.Kind != "raw" || first.Request.Body.Text != `{"user":"alice"}` {
		t.Fatalf("body = %+v", first.Request.Body)
	}
	second := res.Children[1]
	if second.Request.Method != "GET" || second.Request.Url != "https://api.example.com/orders?page=2&size=20" {
		t.Fatalf("second.request = %+v", second.Request)
	}
	third := res.Children[2]
	if third.Request.Method != "DELETE" {
		t.Fatalf("third = %+v", third.Request)
	}
}

func TestRestClientExporter(t *testing.T) {
	col := model.Node{Id: "root", Kind: "collection", Name: "冒烟集合"}
	children := []model.Node{
		{Id: "r1", ParentId: "root", Kind: "request", Name: "登录", SortOrder: 10, Request: &model.HttpRequest{
			Method: "POST", Url: "https://api.example.com/login",
			Headers:  []model.KV{{Key: "Content-Type", Value: "application/json", Enabled: true}},
			Body:     model.Body{Kind: "raw", Text: `{"user":"alice"}`},
			Settings: model.DefaultSettings(),
		}},
		{Id: "f1", ParentId: "root", Kind: "folder", Name: "查询", SortOrder: 20},
		{Id: "r2", ParentId: "f1", Kind: "request", Name: "列表", SortOrder: 30, Request: &model.HttpRequest{
			Method: "GET", Url: "https://api.example.com/orders", Settings: model.DefaultSettings(),
		}},
	}
	out, err := restClientExporter{}.Export(col, children)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "### 登录") || !strings.Contains(out, "POST https://api.example.com/login") {
		t.Fatalf("out =\n%s", out)
	}
	if !strings.Contains(out, `{"user":"alice"}`) {
		t.Fatalf("body missing:\n%s", out)
	}
	// folder 下的请求也导出（缩进层级无关紧要，名字可见即可）
	if !strings.Contains(out, "### 列表") || !strings.Contains(out, "GET https://api.example.com/orders") {
		t.Fatalf("nested request missing:\n%s", out)
	}
}

func TestRestClientRoundTrip(t *testing.T) {
	col := model.Node{Id: "root", Kind: "collection", Name: "rt"}
	children := []model.Node{
		{Id: "r1", ParentId: "root", Kind: "request", Name: "登录", SortOrder: 10, Request: &model.HttpRequest{
			Method: "POST", Url: "https://api.example.com/login?src=web",
			Headers:  []model.KV{{Key: "Content-Type", Value: "application/json", Enabled: true}},
			Body:     model.Body{Kind: "raw", Text: `{"user":"alice"}`},
			Settings: model.DefaultSettings(),
		}},
	}
	text, err := restClientExporter{}.Export(col, children)
	if err != nil {
		t.Fatal(err)
	}
	res, err := restClientImporter{}.Import(text)
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if len(res.Children) != 1 {
		t.Fatalf("children = %d", len(res.Children))
	}
	got := res.Children[0].Request
	if got.Method != "POST" || got.Url != "https://api.example.com/login?src=web" || got.Body.Text != `{"user":"alice"}` {
		t.Fatalf("round-trip = %+v", got)
	}
}
