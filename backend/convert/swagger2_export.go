package convert

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"apirequest/backend/model"
)

// Swagger 2.0 集合导出。
// 与 OpenAPI 3.x 的主要结构差异：
//   - swagger: "2.0"（非 openapi 字段）
//   - host / basePath / schemes 替代 servers
//   - definitions 替代 components.schemas
//   - securityDefinitions 替代 components.securitySchemes
//   - 请求体用 parameters[{in: body, schema}] 替代 requestBody
//   - 无 components 字段

type swagger2Exporter struct{}

func (swagger2Exporter) Format() string { return "swagger2" }

// swPathItem 一条 path 下各 method 的 operation
type swPathItem struct {
	Get     *swOperation `json:"get,omitempty"`
	Post    *swOperation `json:"post,omitempty"`
	Put     *swOperation `json:"put,omitempty"`
	Patch   *swOperation `json:"patch,omitempty"`
	Delete  *swOperation `json:"delete,omitempty"`
	Head    *swOperation `json:"head,omitempty"`
	Options *swOperation `json:"options,omitempty"`
}

type swOperation struct {
	Tags        []string              `json:"tags,omitempty"`
	Summary     string                `json:"summary,omitempty"`
	Description string                `json:"description,omitempty"`
	OperationId string                `json:"operationId,omitempty"`
	Consumes    []string              `json:"consumes,omitempty"`
	Produces    []string              `json:"produces,omitempty"`
	Parameters  []swParameter         `json:"parameters,omitempty"`
	Responses   map[string]swResp     `json:"responses"`
	Security    []map[string][]string `json:"security,omitempty"`
}

type swParameter struct {
	Name     string `json:"name"`
	In       string `json:"in"` // query | header | body | formData
	Required bool   `json:"required"`
	Type     string `json:"type,omitempty"`
	Format   string `json:"format,omitempty"`
	// body 参数用 schema
	Schema      *swSchema `json:"schema,omitempty"`
	Default     string    `json:"default,omitempty"`
	Description string    `json:"description,omitempty"`
}

type swSchema struct {
	Type       string              `json:"type,omitempty"`
	Format     string              `json:"format,omitempty"`
	Properties map[string]swSchema `json:"properties,omitempty"`
	Items      *swSchema           `json:"items,omitempty"`
	Ref        string              `json:"$ref,omitempty"`
}

type swResp struct {
	Description string `json:"description"`
}

type swSecurityScheme struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Name        string `json:"name,omitempty"`
	In          string `json:"in,omitempty"`
	Scheme      string `json:"scheme,omitempty"` // basic
}

type swDoc struct {
	Swagger string `json:"swagger"`
	Info    struct {
		Title       string `json:"title"`
		Description string `json:"description,omitempty"`
		Version     string `json:"version"`
	} `json:"info"`
	Host                string                      `json:"host,omitempty"`
	BasePath            string                      `json:"basePath,omitempty"`
	Schemes             []string                    `json:"schemes,omitempty"`
	Paths               map[string]swPathItem       `json:"paths"`
	Tags                []map[string]string         `json:"tags,omitempty"`
	SecurityDefinitions map[string]swSecurityScheme `json:"securityDefinitions,omitempty"`
}

func (swagger2Exporter) Export(collection model.Node, children []model.Node) (string, error) {
	doc := swDoc{
		Swagger: "2.0",
		Paths:   map[string]swPathItem{},
	}
	doc.Info.Title = collection.Name
	if doc.Info.Title == "" {
		doc.Info.Title = "Exported Collection"
	}
	doc.Info.Version = "1.0.0"

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

	tagSet := map[string]bool{}
	addTag := func(name string) {
		if name != "" && !tagSet[name] {
			tagSet[name] = true
			doc.Tags = append(doc.Tags, map[string]string{"name": name})
		}
	}

	reqs := []model.Node{}
	for _, n := range children {
		if n.Kind == "request" && n.Request != nil {
			reqs = append(reqs, n)
		}
	}
	sort.SliceStable(reqs, func(i, j int) bool { return reqs[i].SortOrder < reqs[j].SortOrder })

	opCounter := map[string]int{}
	for _, n := range reqs {
		r := *n.Request
		method := strings.ToLower(strings.TrimSpace(r.Method))
		if method == "" {
			method = "get"
		}
		path, host, scheme, rawQuery := swSplitUrl(r.Url)

		if host != "" {
			if doc.Host != "" && doc.Host != host {
				return "", fmt.Errorf("swagger 2.0 cannot represent multiple hosts: %s and %s", doc.Host, host)
			}
			doc.Host = host
			if scheme != "" && !containsString(doc.Schemes, scheme) {
				doc.Schemes = append(doc.Schemes, scheme)
			}
		}

		pi := doc.Paths[path]
		op := swOperation{
			Summary:   n.Name,
			Responses: map[string]swResp{"default": {Description: "auto-exported"}},
		}
		if tag := resolveTag(n); tag != "" {
			op.Tags = []string{tag}
			addTag(tag)
		}
		base := method + "_" + oeSanitizeOpId(path)
		opCounter[base]++
		op.OperationId = fmt.Sprintf("%s_%d", base, opCounter[base])

		// URL query → parameters
		seenQuery := map[string]bool{}
		for _, p := range oeQueryItems(rawQuery) {
			if p.Key == "" || seenQuery[p.Key] {
				continue
			}
			seenQuery[p.Key] = true
			op.Parameters = append(op.Parameters, swParameter{
				Name: p.Key, In: "query", Type: "string", Default: p.Value,
			})
		}
		for _, p := range r.Params {
			if !p.Enabled || p.Key == "" || seenQuery[p.Key] {
				continue
			}
			seenQuery[p.Key] = true
			op.Parameters = append(op.Parameters, swParameter{
				Name: p.Key, In: "query", Type: "string", Default: p.Value, Description: p.Description,
			})
		}
		for _, h := range r.Headers {
			if !h.Enabled || h.Key == "" {
				continue
			}
			op.Parameters = append(op.Parameters, swParameter{
				Name: h.Key, In: "header", Type: "string", Default: h.Value, Description: h.Description,
			})
		}

		// body → in:body 参数
		if swBody := buildSwBody(r.Body); len(swBody) > 0 {
			op.Parameters = append(op.Parameters, swBody...)
			if ct := swContentType(r.Body); ct != "" {
				op.Consumes = []string{ct}
			}
		}

		// auth → securityDefinitions
		a := resolveAuth(n, byId, collection)
		if a.Type != "" {
			if sec, name, scheme := swSecurity(a); sec != nil {
				op.Security = []map[string][]string{sec}
				if doc.SecurityDefinitions == nil {
					doc.SecurityDefinitions = map[string]swSecurityScheme{}
				}
				if _, exists := doc.SecurityDefinitions[name]; !exists {
					doc.SecurityDefinitions[name] = scheme
				}
			}
		}

		var slot **swOperation
		switch method {
		case "get":
			slot = &pi.Get
		case "post":
			slot = &pi.Post
		case "put":
			slot = &pi.Put
		case "patch":
			slot = &pi.Patch
		case "delete":
			slot = &pi.Delete
		case "head":
			slot = &pi.Head
		case "options":
			slot = &pi.Options
		default:
			return "", fmt.Errorf("swagger 2.0 cannot represent HTTP method %q", strings.ToUpper(method))
		}
		if *slot != nil {
			return "", fmt.Errorf("swagger 2.0 cannot represent duplicate operation %s %s", strings.ToUpper(method), path)
		}
		*slot = &op
		doc.Paths[path] = pi
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	return string(out), err
}

// swSplitUrl 把 URL 拆为 Swagger 2.0 所需的 path、host、scheme、rawQuery
func swSplitUrl(raw string) (path, host, scheme, rawQuery string) {
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
		return path, "", "", rawQuery
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "/" + strings.TrimPrefix(raw, "/"), "", "", ""
	}
	path = u.EscapedPath()
	if path == "" {
		path = "/"
	}
	return path, u.Host, u.Scheme, u.RawQuery
}

func buildSwBody(b model.Body) []swParameter {
	switch b.Kind {
	case "raw":
		schema := &swSchema{Type: "string"}
		if b.Language == "json" {
			var obj any
			if json.Unmarshal([]byte(b.Text), &obj) == nil {
				schema.Type = oeGuessJSONType(obj)
			}
		}
		return []swParameter{{
			In:       "body",
			Name:     "body",
			Required: b.Text != "",
			Schema:   schema,
		}}
	case "urlencoded":
		parameters := []swParameter{}
		for _, it := range b.Items {
			if !it.Enabled || it.Key == "" {
				continue
			}
			parameters = append(parameters, swParameter{
				Name: it.Key, In: "formData", Type: "string", Default: it.Value,
			})
		}
		return parameters
	case "formdata":
		parameters := []swParameter{}
		for _, it := range b.Items {
			if !it.Enabled || it.Key == "" {
				continue
			}
			typeName := "string"
			if it.Type == "file" {
				typeName = "file"
			}
			parameters = append(parameters, swParameter{
				Name: it.Key, In: "formData", Type: typeName, Default: it.Value,
			})
		}
		return parameters
	case "binary":
		return []swParameter{{
			In:     "body",
			Name:   "body",
			Schema: &swSchema{Type: "string", Format: "binary"},
		}}
	case "graphql":
		return []swParameter{{
			In:   "body",
			Name: "body",
			Schema: &swSchema{
				Type: "object",
				Properties: map[string]swSchema{
					"query":     {Type: "string"},
					"variables": {Type: "object"},
				},
			},
		}}
	}
	return nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func swContentType(b model.Body) string {
	switch b.Kind {
	case "raw":
		return oeContentTypeOf(b.Language)
	case "urlencoded":
		return "application/x-www-form-urlencoded"
	case "formdata":
		return "multipart/form-data"
	case "binary":
		return "application/octet-stream"
	case "graphql":
		return "application/json"
	}
	return ""
}

func swSecurity(a model.Auth) (ref map[string][]string, name string, scheme swSecurityScheme) {
	switch a.Type {
	case "basic":
		return map[string][]string{"basicAuth": {}}, "basicAuth", swSecurityScheme{Type: "basic", Scheme: "basic"}
	case "bearer":
		return map[string][]string{"bearerAuth": {}}, "bearerAuth", swSecurityScheme{Type: "apiKey", In: "header", Name: "Authorization"}
	case "apikey":
		name := a.Params["key"]
		if name == "" {
			name = "apiKey"
		}
		schemeName := name + "_apiKey"
		in := strings.ToLower(a.Params["in"])
		if in != "query" && in != "header" {
			in = "header"
		}
		return map[string][]string{schemeName: {}}, schemeName,
			swSecurityScheme{Type: "apiKey", Name: name, In: in}
	}
	return nil, "", swSecurityScheme{}
}

func init() {
	RegisterExporter(swagger2Exporter{})
}
