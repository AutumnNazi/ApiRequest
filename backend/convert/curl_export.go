package convert

import (
	"encoding/json"
	"net/url"
	"sort"
	"strings"

	"apirequest/backend/model"
)

// cURL 集合导出（docs/interop.md §2.3 的对偶方向）。
// 与 Importer 不同：Importer 解析单条 cURL 命令；Exporter 把集合树导出为
// JSON 数组（每请求一项，含 name/method/url/headers/body/auth），
// 同时附带可读的 shell 脚本（## 注释分隔的 curl 命令块），用户可任选一种使用。
//
// 设计取舍：
//   - 集合导不出单个含 \ 续行的超长 cURL 串；改为多文件可控：
//     shell 脚本里每个请求一段 curl 命令。
//   - 不做变量替换：导出文本里保留 {{var}}（用户自行 jinja/{{}} 处理）。
//   - formdata/file 字段、binary 文件、graphql body 按现有 codegen 同款策略降级。

type curlExporter struct{}

func (curlExporter) Format() string { return "curl" }

// curlItem 单条 cURL 命令的结构化表示（JSON 输出用）
type curlItem struct {
	Name    string     `json:"name"`
	Method  string     `json:"method"`
	Url     string     `json:"url"`
	Headers []model.KV `json:"headers,omitempty"`
	Body    *curlBody  `json:"body,omitempty"`
	Auth    *curlAuth  `json:"auth,omitempty"`
	Script  string     `json:"script,omitempty"` // 可读 shell 脚本里的 curl 命令
}

type curlBody struct {
	Kind      string           `json:"kind"`
	Language  string           `json:"language,omitempty"`
	Text      string           `json:"text,omitempty"`
	Items     []model.FormItem `json:"items,omitempty"`
	Path      string           `json:"path,omitempty"`
	Query     string           `json:"query,omitempty"`
	Variables string           `json:"variables,omitempty"`
}

type curlAuth struct {
	Type   string            `json:"type"`
	Params map[string]string `json:"params,omitempty"`
}

func (curlExporter) Export(collection model.Node, children []model.Node) (string, error) {
	// 仅处理 request 节点；按 SortOrder 排序保稳定
	reqs := []model.Node{}
	for _, n := range children {
		if n.Kind == "request" && n.Request != nil {
			reqs = append(reqs, n)
		}
	}
	sort.Slice(reqs, func(i, j int) bool { return reqs[i].SortOrder < reqs[j].SortOrder })

	// 构造 byId 以支持 Auth 继承解析
	byId := map[string]model.Node{}
	for _, n := range append([]model.Node{collection}, children...) {
		byId[n.Id] = n
	}

	items := make([]curlItem, 0, len(reqs))
	var script strings.Builder
	script.WriteString("#!/bin/sh\n# 集合导出：")
	script.WriteString(collection.Name)
	script.WriteString("\n# 每个 ## 之间是一条 cURL 命令\n\n")

	for _, n := range reqs {
		r := *n.Request
		// 把继承的 Auth 落到 r.Auth：调用方无需关心 collection/folder Auth
		if r.Auth.Type == "" || r.Auth.Type == "inherit" {
			if eff := resolveAuth(n, byId, collection); eff.Type != "" {
				r.Auth = eff
			}
		}
		item := curlItem{
			Name:    n.Name,
			Method:  r.Method,
			Url:     r.Url,
			Headers: filterEnabledKVs(r.Headers),
		}
		if r.Body.Kind != "" && r.Body.Kind != "none" {
			item.Body = &curlBody{
				Kind:      r.Body.Kind,
				Language:  r.Body.Language,
				Text:      r.Body.Text,
				Items:     r.Body.Items,
				Path:      r.Body.Path,
				Query:     r.Body.Query,
				Variables: r.Body.Variables,
			}
		}
		if r.Auth.Type != "" && r.Auth.Type != "none" && r.Auth.Type != "inherit" {
			item.Auth = &curlAuth{Type: r.Auth.Type, Params: r.Auth.Params}
		}
		item.Script = buildCurlCommand(r)
		items = append(items, item)

		// 写入 shell 脚本：分隔注释 + curl
		script.WriteString("## ")
		script.WriteString(n.Name)
		script.WriteString("\n")
		script.WriteString(item.Script)
		script.WriteString("\n\n")
	}

	// 输出 JSON 包装：{collection, items, shell}
	// shell 字段便于用户直接拿走执行；items 用于程序化再处理。
	out := struct {
		Collection string     `json:"collection"`
		Items      []curlItem `json:"items"`
		Shell      string     `json:"shell"`
	}{
		Collection: collection.Name,
		Items:      items,
		Shell:      script.String(),
	}
	b, err := json.MarshalIndent(out, "", "  ")
	return string(b), err
}

func filterEnabledKVs(kvs []model.KV) []model.KV {
	out := make([]model.KV, 0, len(kvs))
	for _, kv := range kvs {
		if kv.Enabled {
			out = append(out, kv)
		}
	}
	return out
}

// buildCurlCommand 把单请求拼成一段可读 cURL 命令（与 codegen 同款语义，独立实现避免循环依赖）
func buildCurlCommand(r model.HttpRequest) string {
	var b strings.Builder
	b.WriteString("curl")
	if m := strings.ToUpper(strings.TrimSpace(r.Method)); m != "" && m != "GET" {
		b.WriteString(" -X " + m)
	}
	b.WriteString(" '" + shellEscape(fullURL(r)) + "'")

	// Content-Type 由 body 推导时补；已有则不重复
	contentType := ""
	for _, h := range r.Headers {
		if !h.Enabled {
			continue
		}
		if strings.EqualFold(h.Key, "Content-Type") {
			contentType = h.Value
		}
		b.WriteString(" \\\n  -H '" + shellEscape(h.Key+": "+h.Value) + "'")
	}

	// auth：常用类型直接对应 curl flag
	if r.Auth.Type == "basic" {
		u := r.Auth.Params["username"]
		p := r.Auth.Params["password"]
		b.WriteString(" \\\n  -u '" + shellEscape(u+":"+p) + "'")
	} else if r.Auth.Type == "bearer" {
		tok := r.Auth.Params["token"]
		b.WriteString(" \\\n  -H 'Authorization: Bearer " + shellEscape(tok) + "'")
	} else if r.Auth.Type == "apikey" {
		// 默认放 header；query/cookie 分别落到 URL/Cookie 头，避免把 API key 放错位置。
		k := r.Auth.Params["key"]
		v := r.Auth.Params["value"]
		in := strings.ToLower(r.Auth.Params["in"])
		if in == "query" {
			// fullURL 已将 query 形式的 API key 合并进 URL。
		} else if in == "cookie" && k != "" {
			b.WriteString(" \\\n  -H 'Cookie: " + shellEscape(k+"="+v) + "'")
		} else if k != "" {
			b.WriteString(" \\\n  -H '" + shellEscape(k+": "+v) + "'")
		}
	}

	// body
	switch r.Body.Kind {
	case "raw":
		if r.Body.Text != "" {
			if contentType == "" {
				ct := oeContentTypeOf(r.Body.Language)
				b.WriteString(" \\\n  -H 'Content-Type: " + ct + "'")
			}
			b.WriteString(" \\\n  -d '" + shellEscape(r.Body.Text) + "'")
		}
	case "urlencoded":
		if contentType == "" {
			b.WriteString(" \\\n  -H 'Content-Type: application/x-www-form-urlencoded'")
		}
		for _, it := range r.Body.Items {
			if !it.Enabled {
				continue
			}
			b.WriteString(" \\\n  -d '" + shellEscape(it.Key+"="+it.Value) + "'")
		}
	case "formdata":
		for _, it := range r.Body.Items {
			if !it.Enabled {
				continue
			}
			if it.Type == "file" {
				b.WriteString(" \\\n  -F '" + shellEscape(it.Key+"=@"+it.Path) + "'")
			} else {
				b.WriteString(" \\\n  -F '" + shellEscape(it.Key+"="+it.Value) + "'")
			}
		}
	case "binary":
		if r.Body.Path != "" {
			b.WriteString(" \\\n  --data-binary '@" + shellEscape(r.Body.Path) + "'")
		}
	case "graphql":
		// GraphQL：POST application/json，body = {"query":"..","variables":..}
		if contentType == "" {
			b.WriteString(" \\\n  -H 'Content-Type: application/json'")
		}
		variables := strings.TrimSpace(r.Body.Variables)
		if variables == "" {
			variables = "{}"
		}
		var rawVariables json.RawMessage
		if json.Valid([]byte(variables)) {
			rawVariables = json.RawMessage(variables)
		} else {
			// 保留非法输入，但把它编码成 JSON string，避免导出的 shell 本身变成非法 JSON。
			rawVariables, _ = json.Marshal(variables)
		}
		gj, _ := json.Marshal(struct {
			Query     string          `json:"query"`
			Variables json.RawMessage `json:"variables"`
		}{Query: r.Body.Query, Variables: rawVariables})
		b.WriteString(" \\\n  -d '" + shellEscape(string(gj)) + "'")
	}

	if !r.Settings.VerifyTLS {
		b.WriteString(" \\\n  -k")
	}
	return b.String()
}

// fullURL 合并 URL 已有 query 与 r.Params 中的 query 条目；
// 已存在于 URL query 中的 key 不再追加（避免重复）。
// 手工拼接保留 raw（不 URL-escape），与文件顶部"保留 {{var}} 给用户自行替换"的设计一致。
func fullURL(r model.HttpRequest) string {
	raw := r.Url
	// 解析 URL 的 query 部分以识别"已存在的 key"（避免与 Params 重复）；不重写 raw query 串。
	existing := map[string]bool{}
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" {
		for k := range u.Query() {
			existing[k] = true
		}
	}
	hasQ := strings.Contains(raw, "?")
	appendParam := func(key, value string) {
		if key == "" || existing[key] {
			return
		}
		sep := "?"
		if hasQ {
			sep = "&"
		}
		raw += sep + key + "=" + value
		hasQ = true
		existing[key] = true
	}
	for _, p := range r.Params {
		if p.Enabled {
			appendParam(p.Key, p.Value)
		}
	}
	if strings.EqualFold(r.Auth.Type, "apikey") && strings.EqualFold(r.Auth.Params["in"], "query") {
		appendParam(r.Auth.Params["key"], r.Auth.Params["value"])
	}
	return raw
}

func shellEscape(s string) string { return strings.ReplaceAll(s, "'", `'\''`) }

func init() {
	RegisterExporter(curlExporter{})
}
