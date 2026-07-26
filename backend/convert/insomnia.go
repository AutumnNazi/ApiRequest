package convert

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"apirequest/backend/model"
)

// Insomnia v4 export 导入（docs/interop.md：导入格式之一）。
// 结构：resources 数组，_type 区分 workspace / request_group / request / environment。

type insomniaImporter struct{}

func (insomniaImporter) Format() string { return "insomnia" }

func (insomniaImporter) Detect(payload string) bool {
	return strings.Contains(payload, `"__export_format"`) ||
		strings.Contains(payload, `"_type": "workspace"`) ||
		strings.Contains(payload, `"_type":"workspace"`)
}

type insomniaExport struct {
	ExportFormat int `json:"__export_format"`
	Resources    []struct {
		Type     string `json:"_type"`
		Id       string `json:"_id"`
		ParentId string `json:"parentId"`
		Name     string `json:"name"`
		// request 字段
		Method  string `json:"method"`
		Url     string `json:"url"`
		Headers []struct {
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
				Name     string `json:"name"`
				Value    string `json:"value"`
				Disabled bool   `json:"disabled"`
				FileName string `json:"fileName"`
				Type     string `json:"type"`
			} `json:"params"`
		} `json:"body"`
		Authentication struct {
			Type     string `json:"type"`
			Username string `json:"username"`
			Password string `json:"password"`
			Token    string `json:"token"`
		} `json:"authentication"`
		// environment 字段
		Data map[string]any `json:"data"`
		// 排序
		MetaSortKey float64 `json:"metaSortKey"`
	} `json:"resources"`
}

func (insomniaImporter) Import(payload string) (*ImportResult, error) {
	var exp insomniaExport
	if err := json.Unmarshal([]byte(payload), &exp); err != nil {
		return nil, &model.AppError{Kind: model.KindImport, Format: "insomnia",
			Detail: "invalid Insomnia export JSON: " + err.Error()}
	}
	if len(exp.Resources) == 0 {
		return nil, &model.AppError{Kind: model.KindImport, Format: "insomnia",
			Detail: "no resources in export"}
	}

	res := &ImportResult{
		Collection: model.Node{Id: "import-root", Kind: "collection", Name: "Imported from Insomnia"},
	}

	// workspace 名作集合名；base environment 的 data 作集合变量
	workspaceId := ""
	for _, r := range exp.Resources {
		if r.Type == "workspace" {
			res.Collection.Name = r.Name
			workspaceId = r.Id
			break
		}
	}
	for _, r := range exp.Resources {
		if r.Type == "environment" && r.ParentId == workspaceId && len(r.Data) > 0 {
			for k, v := range r.Data {
				res.Collection.Variables = append(res.Collection.Variables, model.Variable{
					Key: k, Value: fmt.Sprintf("%v", v), Type: "default", Enabled: true,
				})
			}
			break // 只取 base environment；子环境让用户在环境管理里建
		}
	}
	sort.Slice(res.Collection.Variables, func(i, j int) bool {
		return res.Collection.Variables[i].Key < res.Collection.Variables[j].Key
	})

	// Insomnia 的 {{ _.var }} → {{var}}
	fixVar := func(s string) string {
		s = strings.ReplaceAll(s, "{{ _.", "{{")
		s = strings.ReplaceAll(s, "{{_.", "{{")
		s = strings.ReplaceAll(s, " }}", "}}")
		return s
	}

	// 资源按 parentId 组树：insomnia id → 我们的占位 id
	idMap := map[string]string{workspaceId: res.Collection.Id}
	nodeSeq := 0
	nextId := func() string {
		nodeSeq++
		return fmt.Sprintf("import-%d", nodeSeq)
	}

	type pending struct {
		insomniaParent string
		node           model.Node
	}
	var items []pending
	for _, r := range exp.Resources {
		switch r.Type {
		case "request_group":
			items = append(items, pending{r.ParentId, model.Node{
				Id: nextId(), Kind: "folder", Name: r.Name, SortOrder: r.MetaSortKey,
			}})
			idMap[r.Id] = items[len(items)-1].node.Id
		case "request":
			req := model.HttpRequest{
				Method:   strings.ToUpper(r.Method),
				Url:      fixVar(r.Url),
				Params:   []model.KV{},
				Headers:  []model.KV{},
				Body:     model.Body{Kind: "none"},
				Auth:     model.Auth{Type: "inherit"},
				Settings: model.DefaultSettings(),
			}
			if req.Method == "" {
				req.Method = "GET"
			}
			for _, h := range r.Headers {
				req.Headers = append(req.Headers, model.KV{
					Key: h.Name, Value: fixVar(h.Value), Enabled: !h.Disabled,
				})
			}
			for _, p := range r.Parameters {
				req.Params = append(req.Params, model.KV{
					Key: p.Name, Value: fixVar(p.Value), Enabled: !p.Disabled,
				})
			}
			switch {
			case strings.Contains(r.Body.MimeType, "json"):
				req.Body = model.Body{Kind: "raw", Language: "json", Text: fixVar(r.Body.Text)}
			case strings.Contains(r.Body.MimeType, "x-www-form-urlencoded"):
				items := []model.FormItem{}
				for _, p := range r.Body.Params {
					items = append(items, model.FormItem{
						Key: p.Name, Value: fixVar(p.Value), Type: "text", Enabled: !p.Disabled,
					})
				}
				req.Body = model.Body{Kind: "urlencoded", Items: items}
			case strings.Contains(r.Body.MimeType, "multipart"):
				items := []model.FormItem{}
				for _, p := range r.Body.Params {
					it := model.FormItem{Key: p.Name, Type: "text", Value: fixVar(p.Value), Enabled: !p.Disabled}
					if p.Type == "file" || p.FileName != "" {
						it.Type = "file"
						it.Path = p.FileName
						it.Value = ""
					}
					items = append(items, it)
				}
				req.Body = model.Body{Kind: "formdata", Items: items}
			case strings.Contains(r.Body.MimeType, "graphql"):
				// insomnia graphql body.text 是 {"query":..,"variables":..} JSON
				var gq struct {
					Query     string          `json:"query"`
					Variables json.RawMessage `json:"variables"`
				}
				if json.Unmarshal([]byte(r.Body.Text), &gq) == nil {
					req.Body = model.Body{Kind: "graphql", Query: gq.Query, Variables: string(gq.Variables)}
				}
			case r.Body.Text != "":
				req.Body = model.Body{Kind: "raw", Language: "text", Text: fixVar(r.Body.Text)}
			}
			switch r.Authentication.Type {
			case "basic":
				req.Auth = model.Auth{Type: "basic", Params: map[string]string{
					"username": r.Authentication.Username, "password": r.Authentication.Password,
				}}
			case "bearer":
				req.Auth = model.Auth{Type: "bearer", Params: map[string]string{
					"token": r.Authentication.Token,
				}}
			}

			items = append(items, pending{r.ParentId, model.Node{
				Id: nextId(), Kind: "request", Name: r.Name, SortOrder: r.MetaSortKey, Request: &req,
			}})
			idMap[r.Id] = items[len(items)-1].node.Id
		}
	}

	// 解析 parent（未知父挂根）
	for _, it := range items {
		n := it.node
		if mapped, ok := idMap[it.insomniaParent]; ok {
			n.ParentId = mapped
		} else {
			n.ParentId = res.Collection.Id
		}
		res.Children = append(res.Children, n)
	}
	if len(res.Children) == 0 {
		res.Warnings = append(res.Warnings, "no requests found in export")
	}
	return res, nil
}

func init() { RegisterImporter(insomniaImporter{}) }
