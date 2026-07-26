package convert

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"apirequest/backend/model"
)

// OpenAPI 3.x / Swagger 2 单向导入（docs/interop.md §2.2）

type openapiImporter struct{}

func (openapiImporter) Format() string { return "openapi" }

func (openapiImporter) Detect(payload string) bool {
	trimmed := strings.TrimSpace(payload)
	return strings.Contains(trimmed, `"openapi"`) || strings.Contains(trimmed, `"swagger"`) ||
		strings.HasPrefix(trimmed, "openapi:") || strings.HasPrefix(trimmed, "swagger:") ||
		strings.Contains(trimmed, "\nopenapi:") || strings.Contains(trimmed, "\nswagger:")
}

// oaDoc 只取需要的字段（JSON 与 YAML 双支持）
type oaDoc struct {
	Openapi string `json:"openapi" yaml:"openapi"`
	Swagger string `json:"swagger" yaml:"swagger"`
	Info    struct {
		Title string `json:"title" yaml:"title"`
	} `json:"info" yaml:"info"`
	Servers []struct {
		Url string `json:"url" yaml:"url"`
	} `json:"servers" yaml:"servers"`
	Host     string                            `json:"host" yaml:"host"`         // swagger 2
	BasePath string                            `json:"basePath" yaml:"basePath"` // swagger 2
	Schemes  []string                          `json:"schemes" yaml:"schemes"`   // swagger 2
	Paths    map[string]map[string]json.RawMessage `json:"paths" yaml:"-"`
	// YAML 时 Paths 单独解析
	YamlPaths map[string]map[string]*oaOperation `json:"-" yaml:"paths"`
}

type oaOperation struct {
	OperationId string   `json:"operationId" yaml:"operationId"`
	Summary     string   `json:"summary" yaml:"summary"`
	Tags        []string `json:"tags" yaml:"tags"`
	Parameters  []struct {
		Name     string `json:"name" yaml:"name"`
		In       string `json:"in" yaml:"in"` // query | header | path
		Required bool   `json:"required" yaml:"required"`
	} `json:"parameters" yaml:"parameters"`
	RequestBody *struct {
		Content map[string]struct {
			Example any `json:"example" yaml:"example"`
		} `json:"content" yaml:"content"`
	} `json:"requestBody" yaml:"requestBody"`
}

func (openapiImporter) Import(payload string) (*ImportResult, error) {
	var doc oaDoc
	isJSON := strings.HasPrefix(strings.TrimSpace(payload), "{")
	if isJSON {
		if err := json.Unmarshal([]byte(payload), &doc); err != nil {
			return nil, &model.AppError{Kind: model.KindImport, Format: "openapi",
				Detail: "invalid OpenAPI JSON: " + err.Error()}
		}
	} else {
		if err := yaml.Unmarshal([]byte(payload), &doc); err != nil {
			return nil, &model.AppError{Kind: model.KindImport, Format: "openapi",
				Detail: "invalid OpenAPI YAML: " + err.Error()}
		}
	}
	if doc.Openapi == "" && doc.Swagger == "" {
		return nil, &model.AppError{Kind: model.KindImport, Format: "openapi",
			Detail: "not an OpenAPI/Swagger document"}
	}

	name := doc.Info.Title
	if name == "" {
		name = "Imported API"
	}
	res := &ImportResult{
		Collection: model.Node{Id: "import-root", Kind: "collection", Name: name},
	}

	// 服务器地址 → 集合变量 baseUrl（interop.md：服务器变量落为变量，导入后提示填值）
	baseUrl := ""
	if len(doc.Servers) > 0 {
		baseUrl = doc.Servers[0].Url
	} else if doc.Host != "" {
		scheme := "https"
		if len(doc.Schemes) > 0 {
			scheme = doc.Schemes[0]
		}
		baseUrl = scheme + "://" + doc.Host + doc.BasePath
	}
	if baseUrl == "" {
		baseUrl = "http://localhost"
		res.Warnings = append(res.Warnings, "no servers defined; set {{baseUrl}} manually")
	}
	res.Collection.Variables = []model.Variable{
		{Key: "baseUrl", Value: baseUrl, Type: "default", Enabled: true},
	}

	// 解析 operations（JSON raw / YAML 结构二选一）
	type op struct {
		path, method string
		detail       *oaOperation
	}
	var ops []op
	if isJSON {
		for path, methods := range doc.Paths {
			for method, raw := range methods {
				if !isHttpMethod(method) {
					continue
				}
				var detail oaOperation
				json.Unmarshal(raw, &detail)
				ops = append(ops, op{path: path, method: method, detail: &detail})
			}
		}
	} else {
		for path, methods := range doc.YamlPaths {
			for method, detail := range methods {
				if !isHttpMethod(method) || detail == nil {
					continue
				}
				ops = append(ops, op{path: path, method: method, detail: detail})
			}
		}
	}
	sort.Slice(ops, func(i, j int) bool {
		if ops[i].path != ops[j].path {
			return ops[i].path < ops[j].path
		}
		return ops[i].method < ops[j].method
	})

	// tags → 一级 folder（interop.md）
	folderId := map[string]string{}
	order := 0.0
	nodeSeq := 0
	nextId := func() string {
		nodeSeq++
		return fmt.Sprintf("import-%d", nodeSeq)
	}
	folderFor := func(tag string) string {
		if tag == "" {
			return res.Collection.Id
		}
		if id, ok := folderId[tag]; ok {
			return id
		}
		order += 10
		f := model.Node{
			Id: nextId(), ParentId: res.Collection.Id, Kind: "folder",
			Name: tag, SortOrder: order,
		}
		res.Children = append(res.Children, f)
		folderId[tag] = f.Id
		return f.Id
	}

	for _, o := range ops {
		tag := ""
		if len(o.detail.Tags) > 0 {
			tag = o.detail.Tags[0]
		}
		parent := folderFor(tag)

		reqName := o.detail.OperationId
		if reqName == "" {
			reqName = o.detail.Summary
		}
		if reqName == "" {
			reqName = strings.ToUpper(o.method) + " " + o.path
		}

		// path 参数 {id} → {{id}} 占位（统一变量语法）
		urlPath := strings.NewReplacer("{", "{{", "}", "}}").Replace(o.path)
		req := model.HttpRequest{
			Method:   strings.ToUpper(o.method),
			Url:      "{{baseUrl}}" + urlPath,
			Params:   []model.KV{},
			Headers:  []model.KV{},
			Body:     model.Body{Kind: "none"},
			Auth:     model.Auth{Type: "inherit"},
			Settings: model.DefaultSettings(),
		}
		for _, p := range o.detail.Parameters {
			kv := model.KV{Key: p.Name, Value: "", Enabled: p.Required}
			switch p.In {
			case "query":
				req.Params = append(req.Params, kv)
			case "header":
				req.Headers = append(req.Headers, kv)
			}
		}
		// requestBody：取首个 media-type 的 example（interop.md：oneOf 取首个可用）
		if o.detail.RequestBody != nil {
			for mediaType, content := range o.detail.RequestBody.Content {
				if strings.Contains(mediaType, "json") {
					text := "{}"
					if content.Example != nil {
						if b, err := json.MarshalIndent(content.Example, "", "  "); err == nil {
							text = string(b)
						}
					}
					req.Body = model.Body{Kind: "raw", Language: "json", Text: text}
					break
				}
			}
		}

		order += 10
		res.Children = append(res.Children, model.Node{
			Id: nextId(), ParentId: parent, Kind: "request",
			Name: reqName, SortOrder: order, Request: &req,
		})
	}
	if len(ops) == 0 {
		res.Warnings = append(res.Warnings, "no operations found in paths")
	}
	return res, nil
}

func isHttpMethod(s string) bool {
	switch strings.ToLower(s) {
	case "get", "post", "put", "patch", "delete", "head", "options", "trace":
		return true
	}
	return false
}

func init() { RegisterImporter(openapiImporter{}) }
