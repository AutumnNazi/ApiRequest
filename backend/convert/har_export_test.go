package convert

import (
	"encoding/json"
	"strings"
	"testing"

	"apirequest/backend/model"
)

func TestHarExporter(t *testing.T) {
	col := model.Node{Id: "root", Kind: "collection", Name: "冒烟集合"}
	children := []model.Node{
		{Id: "r1", ParentId: "root", Kind: "request", Name: "登录", SortOrder: 10, Request: &model.HttpRequest{
			Method: "POST", Url: "https://api.example.com/login?src=web",
			Headers:  []model.KV{{Key: "Content-Type", Value: "application/json", Enabled: true}},
			Body:     model.Body{Kind: "raw", Text: `{"user":"alice"}`, Language: "json"},
			Settings: model.DefaultSettings(),
		}},
		{Id: "r2", ParentId: "root", Kind: "request", Name: "列表", SortOrder: 20, Request: &model.HttpRequest{
			Method: "GET", Url: "https://api.example.com/orders", Settings: model.DefaultSettings(),
		}},
	}
	out, err := Export("har", col, children)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	var doc struct {
		Log struct {
			Version string `json:"version"`
			Creator struct {
				Name string `json:"name"`
			} `json:"creator"`
			Entries []struct {
				StartedDateTime string `json:"startedDateTime"`
				Request         struct {
					Method      string `json:"method"`
					Url         string `json:"url"`
					HTTPVersion string `json:"httpVersion"`
					Headers     []struct {
						Name  string `json:"name"`
						Value string `json:"value"`
					} `json:"headers"`
					QueryString []struct {
						Name  string `json:"name"`
						Value string `json:"value"`
					} `json:"queryString"`
					PostData *struct {
						MimeType string `json:"mimeType"`
						Text     string `json:"text"`
					} `json:"postData"`
					HeadersSize int `json:"headersSize"`
					BodySize    int `json:"bodySize"`
				} `json:"request"`
				Response struct {
					Status  int `json:"status"`
					Content struct {
						Size int `json:"size"`
					} `json:"content"`
				} `json:"response"`
			} `json:"entries"`
		} `json:"log"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("output is not valid HAR JSON: %v\n%s", err, out)
	}
	if doc.Log.Version != "1.2" || doc.Log.Creator.Name == "" {
		t.Fatalf("log header = %+v", doc.Log)
	}
	if len(doc.Log.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(doc.Log.Entries))
	}
	first := doc.Log.Entries[0]
	if first.Request.Method != "POST" || first.Request.Url != "https://api.example.com/login?src=web" {
		t.Fatalf("first.request = %+v", first.Request)
	}
	if len(first.Request.QueryString) != 1 || first.Request.QueryString[0].Name != "src" {
		t.Fatalf("queryString = %+v", first.Request.QueryString)
	}
	if first.Request.PostData == nil || first.Request.PostData.Text != `{"user":"alice"}` {
		t.Fatalf("postData = %+v", first.Request.PostData)
	}
	second := doc.Log.Entries[1]
	if second.Request.Method != "GET" || second.Request.PostData != nil {
		t.Fatalf("second = %+v", second.Request)
	}
	// HAR 规范要求 response 存在：导出为 0 占位（无已保存响应）
	if first.Response.Status != 0 {
		t.Fatalf("response stub = %+v", first.Response)
	}
	if !strings.Contains(string(out), `"httpVersion"`) {
		t.Fatal("httpVersion field missing")
	}
}

func TestHarExporterSkipsNonRequests(t *testing.T) {
	col := model.Node{Id: "root", Kind: "collection", Name: "x"}
	children := []model.Node{
		{Id: "f1", ParentId: "root", Kind: "folder", Name: "folder"},
		{Id: "r1", ParentId: "f1", Kind: "request", Name: "ok", SortOrder: 10, Request: &model.HttpRequest{
			Method: "GET", Url: "https://x.test", Settings: model.DefaultSettings(),
		}},
		{Id: "bad", ParentId: "root", Kind: "request", Name: "no-request-obj", Request: nil},
	}
	out, err := Export("har", col, children)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "https://x.test") || strings.Contains(string(out), "no-request-obj") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}
