package convert

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"apirequest/backend/model"
)

// OpenAPI 3.0.3 集合导出（docs/interop.md §2.2 的对偶方向；
// 导入侧的 oaDoc/oaOperation 已占用名字，本文件类型统一加 oeXxx 前缀以解耦）。
// 设计取舍：
//   - 每个请求节点 → 一个 operation（method 小写后作为 path 下的键）。
//   - 用请求 URL 的 path 部分聚合到 paths；同一 path 多方法并存。
//   - query/headers → parameters；body(raw/json) → requestBody；auth → security。
//   - 文件夹仅用作 tags，不展开为 sub-spec；变量不导出（OpenAPI 用 servers 替代）。
//   - 仅保序输出，不写 components/schemas（请求体不可推断反向 schema）。

type openapiExporter struct{}

func (openapiExporter) Format() string { return "openapi" }

// oePathItem 一条 path 下各 method 的 operation
type oePathItem struct {
	Get                   *oeOperation           `json:"get,omitempty"`
	Post                  *oeOperation           `json:"post,omitempty"`
	Put                   *oeOperation           `json:"put,omitempty"`
	Patch                 *oeOperation           `json:"patch,omitempty"`
	Delete                *oeOperation           `json:"delete,omitempty"`
	Head                  *oeOperation           `json:"head,omitempty"`
	Options               *oeOperation           `json:"options,omitempty"`
	Trace                 *oeOperation           `json:"trace,omitempty"`
	XApiRequestAlternates []oeAlternateOperation `json:"x-apirequest-alternates,omitempty"`
}

// OpenAPI 不能表达同一 path+method 的多条示例，也不能表达自定义方法。
// 用 vendor extension 保真保存，标准消费方会安全忽略。
type oeAlternateOperation struct {
	Method    string      `json:"method"`
	Operation oeOperation `json:"operation"`
}

type oeOperation struct {
	Tags        []string              `json:"tags,omitempty"`
	Summary     string                `json:"summary,omitempty"`
	Description string                `json:"description,omitempty"`
	OperationId string                `json:"operationId,omitempty"`
	Parameters  []oeParameter         `json:"parameters,omitempty"`
	RequestBody *oeRequestBody        `json:"requestBody,omitempty"`
	Responses   map[string]oeResponse `json:"responses"`
	Security    []map[string][]string `json:"security,omitempty"`
}

type oeParameter struct {
	Name     string `json:"name"`
	In       string `json:"in"` // query | header
	Required bool   `json:"required"`
	Example  string `json:"example,omitempty"`
	Schema   struct {
		Type string `json:"type"`
	} `json:"schema"`
	Description string `json:"description,omitempty"`
}

type oeRequestBody struct {
	Description string                 `json:"description,omitempty"`
	Required    bool                   `json:"required"`
	Content     map[string]oeMediaType `json:"content"`
}

type oeMediaType struct {
	Schema map[string]any `json:"schema,omitempty"`
}

type oeResponse struct {
	Description string `json:"description"`
}

type oeDoc struct {
	Openapi           string `json:"openapi"`
	JsonSchemaDialect string `json:"jsonSchemaDialect,omitempty"`
	Info              struct {
		Title       string `json:"title"`
		Description string `json:"description,omitempty"`
		Version     string `json:"version"`
	} `json:"info"`
	Servers []struct {
		Url string `json:"url"`
	} `json:"servers,omitempty"`
	Paths      map[string]oePathItem `json:"paths"`
	Tags       []map[string]string   `json:"tags,omitempty"`
	Security   []map[string][]string `json:"security,omitempty"`
	Components struct {
		SecuritySchemes map[string]oeSecurityScheme `json:"securitySchemes,omitempty"`
	} `json:"components,omitempty"`
}

type oeSecurityScheme struct {
	Type         string `json:"type"`
	Description  string `json:"description,omitempty"`
	Name         string `json:"name,omitempty"`         // apiKey
	In           string `json:"in,omitempty"`           // apiKey: header/query/cookie
	Scheme       string `json:"scheme,omitempty"`       // http: basic/bearer
	BearerFormat string `json:"bearerFormat,omitempty"` // bearer: JWT
}

func (openapiExporter) Export(collection model.Node, children []model.Node) (string, error) {
	doc := oeDoc{
		Openapi: "3.0.3",
		Paths:   map[string]oePathItem{},
	}
	doc.Info.Title = collection.Name
	if doc.Info.Title == "" {
		doc.Info.Title = "Imported Collection"
	}
	doc.Info.Version = "1.0.0"

	// folder 名 → tag；tag 去重保序
	tagSet := map[string]bool{}
	addTag := func(name string) {
		if name != "" && !tagSet[name] {
			tagSet[name] = true
			doc.Tags = append(doc.Tags, map[string]string{"name": name})
		}
	}

	// servers：从每个请求 URL 提取 origin，去重保序
	seenServer := map[string]bool{}
	addServer := func(u string) {
		if origin := oeUrlOrigin(u); origin != "" && !seenServer[origin] {
			seenServer[origin] = true
			doc.Servers = append(doc.Servers, struct {
				Url string `json:"url"`
			}{Url: origin})
		}
	}

	byId := map[string]model.Node{}
	for _, n := range append([]model.Node{collection}, children...) {
		byId[n.Id] = n
	}
	resolveTag := func(n model.Node) string {
		if n.Kind == "collection" || n.ParentId == "" || n.ParentId == collection.Id {
			return ""
		}
		p, ok := byId[n.ParentId]
		if !ok {
			return ""
		}
		if p.Kind == "folder" {
			return p.Name
		}
		// 递归向上找
		for p.ParentId != "" && p.ParentId != collection.Id {
			parent, ok := byId[p.ParentId]
			if !ok {
				break
			}
			if parent.Kind == "folder" {
				return parent.Name
			}
			p = parent
		}
		return ""
	}

	// 仅处理 request 节点；按 SortOrder 排序保稳定
	reqs := []model.Node{}
	for _, n := range children {
		if n.Kind == "request" && n.Request != nil {
			reqs = append(reqs, n)
		}
	}
	sort.SliceStable(reqs, func(i, j int) bool { return reqs[i].SortOrder < reqs[j].SortOrder })

	opCounter := map[string]int{} // 重复 operationId 去重
	for _, n := range reqs {
		r := *n.Request
		method := strings.ToLower(strings.TrimSpace(r.Method))
		if method == "" {
			method = "get"
		}
		path, serverUrl, rawQuery := oeSplitUrl(r.Url)
		if serverUrl != "" {
			addServer(serverUrl)
		}

		pi := doc.Paths[path]
		op := oeOperation{
			Summary:   n.Name,
			Responses: map[string]oeResponse{"default": {Description: "auto-exported; response not inferred"}},
		}
		if tag := resolveTag(n); tag != "" {
			op.Tags = []string{tag}
			addTag(tag)
		}
		// operationId = 方法_路径_序号，规避重复
		base := method + "_" + oeSanitizeOpId(path)
		opCounter[base]++
		op.OperationId = fmt.Sprintf("%s_%d", base, opCounter[base])

		// URL 自带 query 与 Params 都映射为 parameters；同名项只保留第一条示例。
		seenQuery := map[string]bool{}
		for _, p := range oeQueryItems(rawQuery) {
			if p.Key == "" || seenQuery[p.Key] {
				continue
			}
			seenQuery[p.Key] = true
			prm := oeParameter{Name: p.Key, In: "query", Required: false, Example: p.Value}
			prm.Schema.Type = "string"
			op.Parameters = append(op.Parameters, prm)
		}
		for _, p := range r.Params {
			if !p.Enabled || p.Key == "" || seenQuery[p.Key] {
				continue
			}
			seenQuery[p.Key] = true
			prm := oeParameter{Name: p.Key, In: "query", Required: false, Example: p.Value, Description: p.Description}
			prm.Schema.Type = "string"
			op.Parameters = append(op.Parameters, prm)
		}
		for _, h := range r.Headers {
			if !h.Enabled || h.Key == "" {
				continue
			}
			prm := oeParameter{Name: h.Key, In: "header", Required: false, Example: h.Value, Description: h.Description}
			prm.Schema.Type = "string"
			op.Parameters = append(op.Parameters, prm)
		}

		if rb := buildOeRequestBody(r.Body); rb != nil {
			op.RequestBody = rb
		}

		// auth → security；同时落 components.securitySchemes 定义。
		// 解析继承链（collection/folder 级 Auth 也会落到每个请求上）
		a := resolveAuth(n, byId, collection)
		if a.Type != "" {
			if sec, schemeName, scheme := oeSecurity(a); sec != nil {
				op.Security = []map[string][]string{sec}
				if scheme.Type != "" {
					if doc.Components.SecuritySchemes == nil {
						doc.Components.SecuritySchemes = map[string]oeSecurityScheme{}
					}
					if _, exists := doc.Components.SecuritySchemes[schemeName]; !exists {
						doc.Components.SecuritySchemes[schemeName] = scheme
					}
				}
			}
		}

		writeStandardOp := func(slot **oeOperation) {
			if *slot == nil {
				*slot = &op
				return
			}
			pi.XApiRequestAlternates = append(pi.XApiRequestAlternates, oeAlternateOperation{
				Method: strings.ToUpper(method), Operation: op,
			})
		}

		switch method {
		case "get":
			writeStandardOp(&pi.Get)
		case "post":
			writeStandardOp(&pi.Post)
		case "put":
			writeStandardOp(&pi.Put)
		case "patch":
			writeStandardOp(&pi.Patch)
		case "delete":
			writeStandardOp(&pi.Delete)
		case "head":
			writeStandardOp(&pi.Head)
		case "options":
			writeStandardOp(&pi.Options)
		case "trace":
			writeStandardOp(&pi.Trace)
		default:
			pi.XApiRequestAlternates = append(pi.XApiRequestAlternates, oeAlternateOperation{
				Method: strings.ToUpper(method), Operation: op,
			})
		}
		doc.Paths[path] = pi
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	return string(out), err
}

// oeSplitUrl 把 URL 拆为合法的 OpenAPI path、origin 与 raw query。
func oeSplitUrl(raw string) (path, origin, rawQuery string) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "{{") || strings.HasPrefix(raw, "{") || !strings.Contains(raw, "://") {
		if hash := strings.IndexByte(raw, '#'); hash >= 0 {
			raw = raw[:hash]
		}
		if query := strings.IndexByte(raw, '?'); query >= 0 {
			rawQuery = raw[query+1:]
			raw = raw[:query]
		}
		if i := strings.Index(raw, "/"); i >= 0 {
			path = raw[i:]
		} else {
			path = "/" + raw
		}
		if path == "" {
			path = "/"
		}
		return path, "", rawQuery
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "/" + strings.TrimPrefix(raw, "/"), "", ""
	}
	path = u.EscapedPath()
	if path == "" {
		path = "/"
	}
	return path, u.Scheme + "://" + u.Host, u.RawQuery
}

func oeUrlOrigin(raw string) string {
	_, o, _ := oeSplitUrl(raw)
	return o
}

func oeQueryItems(rawQuery string) []model.KV {
	if rawQuery == "" {
		return nil
	}
	out := make([]model.KV, 0, strings.Count(rawQuery, "&")+1)
	for _, pair := range strings.Split(rawQuery, "&") {
		parts := strings.SplitN(pair, "=", 2)
		key, err := url.QueryUnescape(parts[0])
		if err != nil {
			key = parts[0]
		}
		value := ""
		if len(parts) == 2 {
			value, err = url.QueryUnescape(parts[1])
			if err != nil {
				value = parts[1]
			}
		}
		out = append(out, model.KV{Key: key, Value: value, Enabled: true})
	}
	return out
}

func oeSanitizeOpId(path string) string {
	s := strings.Trim(path, "/")
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "{", "")
	s = strings.ReplaceAll(s, "}", "")
	s = strings.ReplaceAll(s, "?", "")
	if s == "" {
		return "root"
	}
	return s
}

func buildOeRequestBody(b model.Body) *oeRequestBody {
	switch b.Kind {
	case "raw":
		ct := oeContentTypeOf(b.Language)
		schema := map[string]any{"type": "string"}
		if b.Language == "json" {
			var obj any
			if json.Unmarshal([]byte(b.Text), &obj) == nil {
				schema = map[string]any{"type": oeGuessJSONType(obj)}
			}
		}
		return &oeRequestBody{
			Required: b.Text != "",
			Content:  map[string]oeMediaType{ct: {Schema: schema}},
		}
	case "urlencoded":
		props := map[string]any{}
		for _, it := range b.Items {
			if !it.Enabled {
				continue
			}
			props[it.Key] = map[string]any{"type": "string"}
		}
		return &oeRequestBody{
			Content: map[string]oeMediaType{
				"application/x-www-form-urlencoded": {Schema: map[string]any{
					"type":       "object",
					"properties": props,
				}},
			},
		}
	case "formdata":
		props := map[string]any{}
		for _, it := range b.Items {
			if !it.Enabled {
				continue
			}
			if it.Type == "file" {
				props[it.Key] = map[string]any{"type": "string", "format": "binary"}
			} else {
				props[it.Key] = map[string]any{"type": "string"}
			}
		}
		return &oeRequestBody{
			Content: map[string]oeMediaType{
				"multipart/form-data": {Schema: map[string]any{
					"type":       "object",
					"properties": props,
				}},
			},
		}
	case "binary":
		return &oeRequestBody{
			Content: map[string]oeMediaType{
				"application/octet-stream": {Schema: map[string]any{"type": "string", "format": "binary"}},
			},
		}
	case "graphql":
		return &oeRequestBody{
			Content: map[string]oeMediaType{
				"application/json": {Schema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query":     map[string]any{"type": "string"},
						"variables": map[string]any{"type": "object"},
					},
				}},
			},
		}
	}
	return nil
}

func oeContentTypeOf(lang string) string {
	switch strings.ToLower(lang) {
	case "json":
		return "application/json"
	case "xml":
		return "application/xml"
	case "html":
		return "text/html"
	default:
		return "text/plain"
	}
}

func oeGuessJSONType(v any) string {
	switch v.(type) {
	case bool:
		return "boolean"
	case float64, int, int64:
		return "number"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return "string"
}

// oeSecurity 把内部 Auth 类型映射到 OpenAPI securityScheme 名与定义；
// 返回 (operation.security 引用体, scheme 名, scheme 定义)。OAuth1/AWS 不映射（导出占位跳过）。
func oeSecurity(a model.Auth) (ref map[string][]string, name string, scheme oeSecurityScheme) {
	switch a.Type {
	case "basic":
		return map[string][]string{"basicAuth": {}}, "basicAuth", oeSecurityScheme{Type: "http", Scheme: "basic"}
	case "bearer":
		return map[string][]string{"bearerAuth": {}}, "bearerAuth", oeSecurityScheme{Type: "http", Scheme: "bearer", BearerFormat: "JWT"}
	case "apikey":
		name := a.Params["key"]
		if name == "" {
			name = "apiKey"
		}
		schemeName := name + "_apiKey"
		in := strings.ToLower(a.Params["in"])
		if in != "query" && in != "cookie" {
			in = "header"
		}
		return map[string][]string{schemeName: {}}, schemeName,
			oeSecurityScheme{Type: "apiKey", Name: name, In: in}
	}
	return nil, "", oeSecurityScheme{}
}

func init() {
	RegisterExporter(openapiExporter{})
}
