package convert

import (
	"encoding/json"
	"fmt"
	"strings"

	"apirequest/backend/model"
)

// Postman Collection v2.1 双向转换（字段映射见 docs/interop.md §2.1）

const postmanSchema = "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"

// ── Postman v2.1 JSON 结构（仅需字段）──

type pmCollection struct {
	Info  pmInfo       `json:"info"`
	Item  []pmItem     `json:"item"`
	Event []pmEvent    `json:"event,omitempty"`
	Vars  []pmVariable `json:"variable,omitempty"`
}

type pmInfo struct {
	Name   string `json:"name"`
	Id     string `json:"_postman_id,omitempty"`
	Schema string `json:"schema"`
}

type pmItem struct {
	Name    string       `json:"name"`
	Item    []pmItem     `json:"item,omitempty"`    // folder
	Request *pmRequest   `json:"request,omitempty"` // request
	Event   []pmEvent    `json:"event,omitempty"`
	Vars    []pmVariable `json:"variable,omitempty"`
}

type pmRequest struct {
	Method string  `json:"method"`
	Url    pmUrl   `json:"url"`
	Header []pmKV  `json:"header,omitempty"`
	Body   *pmBody `json:"body,omitempty"`
	Auth   *pmAuth `json:"auth,omitempty"`
}

// pmUrl 可能是字符串或对象：自定义 UnmarshalJSON 兼容两者
type pmUrl struct {
	Raw   string `json:"raw"`
	Query []pmKV `json:"query,omitempty"`
}

func (u *pmUrl) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		return json.Unmarshal(data, &u.Raw)
	}
	type alias pmUrl
	return json.Unmarshal(data, (*alias)(u))
}

func (u pmUrl) MarshalJSON() ([]byte, error) {
	type alias pmUrl
	return json.Marshal(alias(u))
}

type pmKV struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Disabled bool   `json:"disabled,omitempty"`
	Desc     string `json:"description,omitempty"`
}

type pmBody struct {
	Mode       string      `json:"mode"` // raw | urlencoded | formdata | file | graphql
	Raw        string      `json:"raw,omitempty"`
	Urlencoded []pmKV      `json:"urlencoded,omitempty"`
	Formdata   []pmFormKV  `json:"formdata,omitempty"`
	File       *pmFile     `json:"file,omitempty"`
	Graphql    *pmGraphql  `json:"graphql,omitempty"`
	Options    *pmBodyOpts `json:"options,omitempty"`
}

type pmFormKV struct {
	Key      string `json:"key"`
	Value    string `json:"value,omitempty"`
	Src      string `json:"src,omitempty"`
	Type     string `json:"type,omitempty"` // text | file
	Disabled bool   `json:"disabled,omitempty"`
}

type pmFile struct {
	Src string `json:"src"`
}

type pmGraphql struct {
	Query     string `json:"query"`
	Variables string `json:"variables,omitempty"`
}

type pmBodyOpts struct {
	Raw struct {
		Language string `json:"language,omitempty"`
	} `json:"raw,omitempty"`
}

type pmAuth struct {
	Type   string `json:"type"`
	Basic  []pmKV `json:"basic,omitempty"`
	Bearer []pmKV `json:"bearer,omitempty"`
	Apikey []pmKV `json:"apikey,omitempty"`
	Digest []pmKV `json:"digest,omitempty"`
	Oauth1 []pmKV `json:"oauth1,omitempty"`
	Awsv4  []pmKV `json:"awsv4,omitempty"`
}

type pmEvent struct {
	Listen string   `json:"listen"` // prerequest | test
	Script pmScript `json:"script"`
}

type pmScript struct {
	Exec []string `json:"exec"`
	Type string   `json:"type,omitempty"`
}

type pmVariable struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	Disabled bool   `json:"disabled,omitempty"`
}

// ── Importer ──

type postmanImporter struct{}

func (postmanImporter) Format() string { return "postman" }

func (postmanImporter) Detect(payload string) bool {
	return strings.Contains(payload, "_postman_id") ||
		strings.Contains(payload, "schema.getpostman.com")
}

func (postmanImporter) Import(payload string) (*ImportResult, error) {
	var col pmCollection
	if err := json.Unmarshal([]byte(payload), &col); err != nil {
		return nil, &model.AppError{Kind: model.KindImport, Format: "postman",
			Detail: "invalid Postman collection JSON: " + err.Error()}
	}
	if col.Info.Name == "" {
		col.Info.Name = "Imported Collection"
	}

	res := &ImportResult{
		Collection: model.Node{
			Kind:      "collection",
			Name:      col.Info.Name,
			Variables: importVars(col.Vars),
		},
	}
	pre, test := importEvents(col.Event)
	res.Collection.PreScript = pre
	res.Collection.TestScript = test
	// 根 id 用占位（落库时重新生成，避免与既有冲突——见 interop.md：id 冲突重生成）
	res.Collection.Id = "import-root"

	var walk func(items []pmItem, parentId string, order *float64)
	walk = func(items []pmItem, parentId string, order *float64) {
		for _, it := range items {
			*order += 10
			n := model.Node{
				Id:        fmt.Sprintf("import-%d", len(res.Children)+1),
				ParentId:  parentId,
				Name:      it.Name,
				SortOrder: *order,
				Variables: importVars(it.Vars),
			}
			pre, test := importEvents(it.Event)
			n.PreScript = pre
			n.TestScript = test

			if it.Request != nil {
				n.Kind = "request"
				req := importRequest(*it.Request, res)
				// 请求级脚本挂到 HttpRequest 上
				req.PreScript = pre
				req.TestScript = test
				n.PreScript, n.TestScript = "", ""
				n.Request = &req
			} else {
				n.Kind = "folder"
			}
			res.Children = append(res.Children, n)
			if len(it.Item) > 0 {
				sub := 0.0
				walk(it.Item, n.Id, &sub)
			}
		}
	}
	order := 0.0
	walk(col.Item, res.Collection.Id, &order)
	return res, nil
}

func importVars(vars []pmVariable) []model.Variable {
	out := make([]model.Variable, 0, len(vars))
	for _, v := range vars {
		out = append(out, model.Variable{Key: v.Key, Value: v.Value, Type: "default", Enabled: !v.Disabled})
	}
	return out
}

func importEvents(events []pmEvent) (pre, test string) {
	for _, e := range events {
		code := strings.Join(e.Script.Exec, "\n")
		switch e.Listen {
		case "prerequest":
			pre = code
		case "test":
			test = code
		}
	}
	return
}

func importRequest(pr pmRequest, res *ImportResult) model.HttpRequest {
	req := model.HttpRequest{
		Method:   pr.Method,
		Url:      pr.Url.Raw,
		Params:   []model.KV{},
		Headers:  importKVs(pr.Header),
		Body:     model.Body{Kind: "none"},
		Auth:     model.Auth{Type: "inherit"},
		Settings: model.DefaultSettings(),
	}
	if req.Method == "" {
		req.Method = "GET"
	}
	// query：url.query 数组优先（与 raw 中的重复由发送时合并逻辑处理，这里剥离 raw 的 query 段）
	if len(pr.Url.Query) > 0 {
		req.Params = importKVs(pr.Url.Query)
		if i := strings.Index(req.Url, "?"); i >= 0 {
			req.Url = req.Url[:i]
		}
	}
	if pr.Body != nil {
		req.Body = importBody(*pr.Body, res)
	}
	if pr.Auth != nil {
		req.Auth = importAuth(*pr.Auth)
	}
	return req
}

func importKVs(kvs []pmKV) []model.KV {
	out := make([]model.KV, 0, len(kvs))
	for _, kv := range kvs {
		out = append(out, model.KV{Key: kv.Key, Value: kv.Value, Enabled: !kv.Disabled, Description: kv.Desc})
	}
	return out
}

func importBody(b pmBody, res *ImportResult) model.Body {
	switch b.Mode {
	case "raw":
		lang := "text"
		if b.Options != nil && b.Options.Raw.Language != "" {
			lang = b.Options.Raw.Language
		} else if strings.HasPrefix(strings.TrimSpace(b.Raw), "{") {
			lang = "json"
		}
		if lang != "json" && lang != "xml" && lang != "html" && lang != "text" {
			lang = "text"
		}
		return model.Body{Kind: "raw", Language: lang, Text: b.Raw}
	case "urlencoded":
		items := make([]model.FormItem, 0, len(b.Urlencoded))
		for _, kv := range b.Urlencoded {
			items = append(items, model.FormItem{Key: kv.Key, Value: kv.Value, Type: "text", Enabled: !kv.Disabled})
		}
		return model.Body{Kind: "urlencoded", Items: items}
	case "formdata":
		items := make([]model.FormItem, 0, len(b.Formdata))
		for _, kv := range b.Formdata {
			it := model.FormItem{Key: kv.Key, Enabled: !kv.Disabled, Type: "text", Value: kv.Value}
			if kv.Type == "file" {
				it.Type = "file"
				it.Path = kv.Src
			}
			items = append(items, it)
		}
		return model.Body{Kind: "formdata", Items: items}
	case "file":
		if b.File != nil {
			return model.Body{Kind: "binary", Path: b.File.Src}
		}
	case "graphql":
		if b.Graphql != nil {
			return model.Body{Kind: "graphql", Query: b.Graphql.Query, Variables: b.Graphql.Variables}
		}
	case "":
	default:
		res.Warnings = append(res.Warnings, "unsupported body mode: "+b.Mode)
	}
	return model.Body{Kind: "none"}
}

// importAuth Postman 的参数数组转对象（interop.md：type 名对齐，参数数组转对象）
func importAuth(a pmAuth) model.Auth {
	kvToMap := func(kvs []pmKV) map[string]string {
		m := map[string]string{}
		for _, kv := range kvs {
			m[kv.Key] = kv.Value
		}
		return m
	}
	switch a.Type {
	case "basic":
		return model.Auth{Type: "basic", Params: kvToMap(a.Basic)}
	case "bearer":
		return model.Auth{Type: "bearer", Params: kvToMap(a.Bearer)}
	case "apikey":
		m := kvToMap(a.Apikey)
		// Postman 用 in=header/query，key/value 同名
		return model.Auth{Type: "apikey", Params: m}
	case "digest":
		return model.Auth{Type: "digest", Params: kvToMap(a.Digest)}
	case "oauth1":
		return model.Auth{Type: "oauth1", Params: kvToMap(a.Oauth1)}
	case "awsv4":
		return model.Auth{Type: "awsv4", Params: kvToMap(a.Awsv4)}
	case "noauth":
		return model.Auth{Type: "none"}
	default:
		return model.Auth{Type: "inherit"}
	}
}

// ── Exporter ──

type postmanExporter struct{}

func (postmanExporter) Format() string { return "postman" }

func (postmanExporter) Export(collection model.Node, children []model.Node) (string, error) {
	col := pmCollection{
		Info: pmInfo{Name: collection.Name, Schema: postmanSchema},
		Vars: exportVars(collection.Variables),
	}
	col.Event = exportEvents(collection.PreScript, collection.TestScript)

	byParent := map[string][]model.Node{}
	byId := map[string]model.Node{collection.Id: collection}
	for _, n := range children {
		byParent[n.ParentId] = append(byParent[n.ParentId], n)
		byId[n.Id] = n
	}
	var build func(parentId string) []pmItem
	build = func(parentId string) []pmItem {
		var items []pmItem
		for _, n := range byParent[parentId] {
			it := pmItem{Name: n.Name, Vars: exportVars(n.Variables)}
			if n.Kind == "request" && n.Request != nil {
				request := *n.Request
				if request.Auth.Type == "" || request.Auth.Type == "inherit" {
					request.Auth = resolveAuth(n, byId, collection)
				}
				r := exportRequest(request)
				it.Request = &r
				it.Event = exportEvents(n.Request.PreScript, n.Request.TestScript)
			} else {
				it.Event = exportEvents(n.PreScript, n.TestScript)
				it.Item = build(n.Id)
			}
			items = append(items, it)
		}
		return items
	}
	col.Item = build(collection.Id)

	out, err := json.MarshalIndent(col, "", "  ")
	return string(out), err
}

func exportVars(vars []model.Variable) []pmVariable {
	var out []pmVariable
	for _, v := range vars {
		out = append(out, pmVariable{Key: v.Key, Value: v.Value, Disabled: !v.Enabled})
	}
	return out
}

func exportEvents(pre, test string) []pmEvent {
	var out []pmEvent
	if pre != "" {
		out = append(out, pmEvent{Listen: "prerequest", Script: pmScript{Exec: strings.Split(pre, "\n"), Type: "text/javascript"}})
	}
	if test != "" {
		out = append(out, pmEvent{Listen: "test", Script: pmScript{Exec: strings.Split(test, "\n"), Type: "text/javascript"}})
	}
	return out
}

func exportRequest(r model.HttpRequest) pmRequest {
	rawUrl := r.Url
	var query []pmKV
	for _, p := range r.Params {
		query = append(query, pmKV{Key: p.Key, Value: p.Value, Disabled: !p.Enabled})
	}
	pr := pmRequest{
		Method: r.Method,
		Url:    pmUrl{Raw: rawUrl, Query: query},
		Header: exportKVs(r.Headers),
	}
	switch r.Body.Kind {
	case "raw":
		b := &pmBody{Mode: "raw", Raw: r.Body.Text, Options: &pmBodyOpts{}}
		b.Options.Raw.Language = r.Body.Language
		pr.Body = b
	case "urlencoded":
		b := &pmBody{Mode: "urlencoded"}
		for _, it := range r.Body.Items {
			b.Urlencoded = append(b.Urlencoded, pmKV{Key: it.Key, Value: it.Value, Disabled: !it.Enabled})
		}
		pr.Body = b
	case "formdata":
		b := &pmBody{Mode: "formdata"}
		for _, it := range r.Body.Items {
			f := pmFormKV{Key: it.Key, Type: it.Type, Disabled: !it.Enabled}
			if it.Type == "file" {
				f.Src = it.Path
			} else {
				f.Value = it.Value
			}
			b.Formdata = append(b.Formdata, f)
		}
		pr.Body = b
	case "binary":
		pr.Body = &pmBody{Mode: "file", File: &pmFile{Src: r.Body.Path}}
	case "graphql":
		pr.Body = &pmBody{Mode: "graphql", Graphql: &pmGraphql{Query: r.Body.Query, Variables: r.Body.Variables}}
	}
	if r.Auth.Type != "" && r.Auth.Type != "inherit" {
		pr.Auth = exportAuth(r.Auth)
	}
	return pr
}

func exportKVs(kvs []model.KV) []pmKV {
	var out []pmKV
	for _, kv := range kvs {
		out = append(out, pmKV{Key: kv.Key, Value: kv.Value, Disabled: !kv.Enabled, Desc: kv.Description})
	}
	return out
}

func exportAuth(a model.Auth) *pmAuth {
	mapToKV := func(m map[string]string) []pmKV {
		var out []pmKV
		for k, v := range m {
			out = append(out, pmKV{Key: k, Value: v})
		}
		return out
	}
	pa := &pmAuth{Type: a.Type}
	switch a.Type {
	case "none":
		pa.Type = "noauth"
	case "basic":
		pa.Basic = mapToKV(a.Params)
	case "bearer":
		pa.Bearer = mapToKV(a.Params)
	case "apikey":
		pa.Apikey = mapToKV(a.Params)
	case "digest":
		pa.Digest = mapToKV(a.Params)
	case "oauth1":
		pa.Oauth1 = mapToKV(a.Params)
	case "awsv4":
		pa.Awsv4 = mapToKV(a.Params)
	}
	return pa
}

func init() {
	RegisterImporter(postmanImporter{})
	RegisterExporter(postmanExporter{})
}
