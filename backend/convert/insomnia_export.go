// Insomnia v4 导出（docs/interop.md）：与导入器对称反转。
// 结构：workspace（= 集合）+ base environment（= 集合变量）+
// request_group（= 文件夹）+ request（= 请求），parentId 串树、metaSortKey 保序。
package convert

import (
	"encoding/json"
	"fmt"
	"sort"

	"apirequest/backend/model"
)

type insomniaExporter struct{}

func (insomniaExporter) Format() string { return "insomnia" }

// insomniaResource Insomnia 导出条目的统一形状（只写导入端认得的字段）
type insomniaResource struct {
	Type     string `json:"_type"`
	Id       string `json:"_id"`
	ParentId string `json:"parentId,omitempty"`
	Name     string `json:"name,omitempty"`
	// request 字段
	Method  string `json:"method,omitempty"`
	Url     string `json:"url,omitempty"`
	Headers []struct {
		Name     string `json:"name"`
		Value    string `json:"value"`
		Disabled bool   `json:"disabled"`
	} `json:"headers,omitempty"`
	Parameters []struct {
		Name     string `json:"name"`
		Value    string `json:"value"`
		Disabled bool   `json:"disabled"`
	} `json:"parameters,omitempty"`
	Body struct {
		MimeType string `json:"mimeType,omitempty"`
		Text     string `json:"text,omitempty"`
		Params   []struct {
			Name     string `json:"name"`
			Value    string `json:"value"`
			Disabled bool   `json:"disabled"`
		} `json:"params,omitempty"`
	} `json:"body,omitempty"`
	Authentication struct {
		Type     string `json:"type"`
		Username string `json:"username,omitempty"`
		Password string `json:"password,omitempty"`
		Token    string `json:"token,omitempty"`
	} `json:"authentication,omitempty"`
	// environment 字段
	Data map[string]any `json:"data,omitempty"`
	// 排序（树序）
	MetaSortKey float64 `json:"metaSortKey"`
}

func (insomniaExporter) Export(collection model.Node, children []model.Node) (string, error) {
	byParent := map[string][]model.Node{}
	byId := map[string]model.Node{collection.Id: collection}
	for _, n := range children {
		byParent[n.ParentId] = append(byParent[n.ParentId], n)
		byId[n.Id] = n
	}
	// children 顺序按 SortOrder 稳定：树序决定 metaSortKey
	for _, list := range byParent {
		sort.SliceStable(list, func(i, j int) bool { return list[i].SortOrder < list[j].SortOrder })
	}

	resources := []insomniaResource{}
	workspace := insomniaResource{Type: "workspace", Id: "wrk_1", Name: collection.Name}
	resources = append(resources, workspace)

	// 集合变量 → base environment（parentId 挂 workspace）
	if len(collection.Variables) > 0 {
		env := insomniaResource{Type: "environment", Id: "env_1", ParentId: workspace.Id,
			Name: "Base Environment", Data: map[string]any{}}
		for _, v := range collection.Variables {
			if v.Enabled {
				env.Data[v.Key] = v.Value
			}
		}
		resources = append(resources, env)
	}

	sortKey := 0.0
	var build func(parentId, insomniaParent string)
	build = func(parentId, insomniaParent string) {
		for _, n := range byParent[parentId] {
			sortKey += 10
			if n.Kind == "request" && n.Request != nil {
				req := *n.Request
				// inherit auth 在导出侧解析成具体类型（Insomnia 无继承概念）
				if req.Auth.Type == "" || req.Auth.Type == "inherit" {
					req.Auth = resolveAuth(n, byId, collection)
				}
				resources = append(resources, insomniaRequest(n, insomniaParent, req, sortKey))
				continue
			}
			group := insomniaResource{Type: "request_group", Id: "grp_" + n.Id,
				ParentId: insomniaParent, Name: n.Name, MetaSortKey: sortKey}
			resources = append(resources, group)
			build(n.Id, group.Id)
		}
	}
	build(collection.Id, workspace.Id)

	out := struct {
		ExportFormat int                 `json:"__export_format"`
		Resources    []insomniaResource  `json:"resources"`
	}{ExportFormat: 4, Resources: resources}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func insomniaRequest(n model.Node, parent string, r model.HttpRequest, sortKey float64) insomniaResource {
	res := insomniaResource{
		Type: "request", Id: "req_" + n.Id, ParentId: parent,
		Name: n.Name, Method: r.Method, Url: r.Url, MetaSortKey: sortKey,
	}
	for _, h := range r.Headers {
		res.Headers = append(res.Headers, struct {
			Name     string `json:"name"`
			Value    string `json:"value"`
			Disabled bool   `json:"disabled"`
		}{Name: h.Key, Value: h.Value, Disabled: !h.Enabled})
	}
	for _, p := range r.Params {
		res.Parameters = append(res.Parameters, struct {
			Name     string `json:"name"`
			Value    string `json:"value"`
			Disabled bool   `json:"disabled"`
		}{Name: p.Key, Value: p.Value, Disabled: !p.Enabled})
	}
	switch r.Body.Kind {
	case "raw":
		mime := "text/plain"
		switch r.Body.Language {
		case "json":
			mime = "application/json"
		case "xml":
			mime = "application/xml"
		case "html":
			mime = "text/html"
		}
		res.Body.MimeType = mime
		res.Body.Text = r.Body.Text
	case "urlencoded":
		res.Body.MimeType = "application/x-www-form-urlencoded"
		for _, it := range r.Body.Items {
			res.Body.Params = append(res.Body.Params, struct {
				Name     string `json:"name"`
				Value    string `json:"value"`
				Disabled bool   `json:"disabled"`
			}{Name: it.Key, Value: it.Value, Disabled: !it.Enabled})
		}
	case "formdata":
		res.Body.MimeType = "multipart/form-data"
		for _, it := range r.Body.Items {
			// Insomnia 文件参数：value 存路径（fileName 由导入端读）
			value := it.Value
			if it.Type == "file" {
				value = it.Path
			}
			res.Body.Params = append(res.Body.Params, struct {
				Name     string `json:"name"`
				Value    string `json:"value"`
				Disabled bool   `json:"disabled"`
			}{Name: it.Key, Value: value, Disabled: !it.Enabled})
		}
	case "graphql":
		res.Body.MimeType = "application/graphql"
		res.Body.Text = fmt.Sprintf(`{"query":%s,"variables":%s}`,
			jsonString(r.Body.Query), jsonString(orDefault(r.Body.Variables, "{}")))
	}
	switch r.Auth.Type {
	case "basic":
		res.Authentication.Type = "basic"
		res.Authentication.Username = r.Auth.Params["username"]
		res.Authentication.Password = r.Auth.Params["password"]
	case "bearer":
		res.Authentication.Type = "bearer"
		res.Authentication.Token = r.Auth.Params["token"]
	default:
		res.Authentication.Type = "none"
	}
	return res
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func init() { RegisterExporter(insomniaExporter{}) }
