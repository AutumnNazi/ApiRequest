package convert

import (
	"encoding/json"
	"testing"

	"apirequest/backend/model"
)

// Insomnia v4 导出：与导入器对称（docs/interop.md 互操作矩阵补齐）
func TestInsomniaExporterRoundTrip(t *testing.T) {
	collection := model.Node{
		Id: "col", Kind: "collection", Name: "demo",
		Variables: []model.Variable{{Key: "base", Value: "https://api.test", Type: "default", Enabled: true}},
	}
	children := []model.Node{
		{Id: "f1", ParentId: "col", Kind: "folder", Name: "Folder A", SortOrder: 10},
		{Id: "r1", ParentId: "f1", Kind: "request", Name: "Create user", SortOrder: 1,
			Request: &model.HttpRequest{
				Method: "POST", Url: "{{base}}/users",
				Headers: []model.KV{{Key: "Content-Type", Value: "application/json", Enabled: true}},
				Body:   model.Body{Kind: "raw", Language: "json", Text: `{"n":1}`},
				Auth:   model.Auth{Type: "basic", Params: map[string]string{"username": "u", "password": "p"}},
				Settings: model.DefaultSettings(),
			}},
		{Id: "r2", ParentId: "col", Kind: "request", Name: "List", SortOrder: 2,
			Request: &model.HttpRequest{
				Method: "GET", Url: "https://api.test/items?page=2",
				Params: []model.KV{{Key: "page", Value: "2", Enabled: true}},
				Body:   model.Body{Kind: "urlencoded", Items: []model.FormItem{{Key: "a", Value: "1", Type: "text", Enabled: true}}},
				Settings: model.DefaultSettings(),
			}},
	}

	out, err := Export("insomnia", collection, children)
	if err != nil {
		t.Fatalf("export: %v", err)
	}

	// 结构断言：__export_format=4、workspace 资源、request 挂对父
	var payload struct {
		ExportFormat int `json:"__export_format"`
		Resources    []struct {
			Type     string `json:"_type"`
			Id       string `json:"_id"`
			ParentId string `json:"parentId"`
			Name     string `json:"name"`
			Method   string `json:"method"`
			Url      string `json:"url"`
			Headers  []struct {
				Name     string `json:"name"`
				Value    string `json:"value"`
				Disabled bool   `json:"disabled"`
			} `json:"headers"`
			Parameters []struct {
				Name     string `json:"name"`
				Value    string `json:"value"`
				Disabled bool   `json:"disabled"`
			} `json:"parameters"`
			Body struct {
				MimeType string `json:"mimeType"`
				Text     string `json:"text"`
				Params   []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
				} `json:"params"`
			} `json:"body"`
			Authentication struct {
				Type     string `json:"type"`
				Username string `json:"username"`
				Password string `json:"password"`
			} `json:"authentication"`
			MetaSortKey float64 `json:"metaSortKey"`
			Data        map[string]any `json:"data"`
		} `json:"resources"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("parse export: %v\n%s", err, out)
	}
	if payload.ExportFormat != 4 {
		t.Fatalf("__export_format = %d, want 4", payload.ExportFormat)
	}
	var workspace, group, req1, req2, env *struct {
		Type     string `json:"_type"`
		Id       string `json:"_id"`
		ParentId string `json:"parentId"`
		Name     string `json:"name"`
		Method   string `json:"method"`
		Url      string `json:"url"`
		Headers  []struct {
			Name     string `json:"name"`
			Value    string `json:"value"`
			Disabled bool   `json:"disabled"`
		} `json:"headers"`
		Parameters []struct {
			Name     string `json:"name"`
			Value    string `json:"value"`
			Disabled bool   `json:"disabled"`
		} `json:"parameters"`
		Body struct {
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
			Params   []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"params"`
		} `json:"body"`
		Authentication struct {
			Type     string `json:"type"`
			Username string `json:"username"`
			Password string `json:"password"`
		} `json:"authentication"`
		MetaSortKey float64      `json:"metaSortKey"`
		Data        map[string]any `json:"data"`
	}
	for i := range payload.Resources {
		r := &payload.Resources[i]
		switch {
		case r.Type == "workspace":
			workspace = r
		case r.Type == "request_group":
			group = r
		case r.Type == "request" && r.Name == "Create user":
			req1 = r
		case r.Type == "request" && r.Name == "List":
			req2 = r
		case r.Type == "environment":
			env = r
		}
	}
	if workspace == nil || workspace.Name != "demo" {
		t.Fatalf("workspace resource missing or wrong name: %+v", workspace)
	}
	if group == nil || group.Name != "Folder A" || group.ParentId != workspace.Id {
		t.Fatalf("request_group wrong: %+v", group)
	}
	if req1 == nil || req1.ParentId != group.Id {
		t.Fatalf("request r1 not nested under folder: %+v", req1)
	}
	if req1.Method != "POST" || req1.Url != "{{base}}/users" {
		t.Fatalf("request r1 method/url = %s %s", req1.Method, req1.Url)
	}
	if req1.Body.MimeType != "application/json" || req1.Body.Text != `{"n":1}` {
		t.Fatalf("request r1 body = %+v", req1.Body)
	}
	if req1.Authentication.Type != "basic" || req1.Authentication.Username != "u" {
		t.Fatalf("request r1 auth = %+v", req1.Authentication)
	}
	if req2 == nil || req2.ParentId != workspace.Id {
		t.Fatalf("request r2 must sit at workspace root: %+v", req2)
	}
	if req2.Body.MimeType != "application/x-www-form-urlencoded" || len(req2.Body.Params) != 1 || req2.Body.Params[0].Name != "a" {
		t.Fatalf("request r2 urlencoded body = %+v", req2.Body)
	}
	// 集合变量 → base environment
	if env == nil || env.ParentId != workspace.Id || env.Data["base"] != "https://api.test" {
		t.Fatalf("base environment missing vars: %+v", env)
	}

	// 回程验证：我们的导出可以被我们自己的导入器吃回去
	imported, err := Import("insomnia", out)
	if err != nil {
		t.Fatalf("re-import own export: %v", err)
	}
	if imported.Collection.Name != "demo" {
		t.Fatalf("re-imported name = %q", imported.Collection.Name)
	}
	if len(imported.Children) != 3 {
		t.Fatalf("re-imported children = %d, want 3", len(imported.Children))
	}
	var found bool
	for _, n := range imported.Children {
		if n.Name == "Create user" && n.Request != nil && n.Request.Method == "POST" {
			found = true
		}
	}
	if !found {
		t.Fatal("re-import lost the POST request")
	}
}
