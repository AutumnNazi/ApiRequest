// Package codegen 实现代码生成器（docs/interop.md §3）。
// IR(HttpRequest) → 目标语言片段；按 id 注册（docs/extensibility.md）。
package codegen

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"apirequest/backend/model"
)

// Generator 代码生成器接口
type Generator interface {
	Id() string   // "curl" / "javascript-fetch" / ...
	Name() string // 展示名
	Generate(req model.HttpRequest) string
}

var registry = map[string]Generator{}
var order []string

// Register 注册生成器（init 时调用）
func Register(g Generator) {
	registry[g.Id()] = g
	order = append(order, g.Id())
}

// Target 生成目标描述（前端列表用）
type Target struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

// Targets 返回全部目标（注册顺序）
func Targets() []Target {
	out := make([]Target, 0, len(order))
	sort.Strings(order)
	for _, id := range order {
		out = append(out, Target{Id: id, Name: registry[id].Name()})
	}
	return out
}

// Generate 按目标 id 生成
func Generate(id string, req model.HttpRequest) (string, error) {
	g, ok := registry[id]
	if !ok {
		return "", model.NewError(model.KindValidation, "unknown codegen target: "+id)
	}
	return g.Generate(req), nil
}

// ── 共用辅助 ──

// fullUrl 合并 url 与启用的 query 参数。
// 与 curl_export.fullURL 行为对齐：
//   - 已在 URL query 中出现的 key 不再追加（避免重复，对 Postman 风格同 key 多值不丢）
//   - URL 解析失败（如 {{base}}/path）回退 raw 拼接，保留 {{var}} 供前端模板引擎替换
//   - 仍然对 Params 的 key/value 做 url.QueryEscape（codegen 输出代码片段需要合法 URL 字面）
func fullUrl(req model.HttpRequest) string {
	raw := req.Url
	existing := map[string]bool{}
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" {
		for k := range u.Query() {
			existing[k] = true
		}
	}
	hasQ := strings.Contains(raw, "?")
	for _, p := range req.Params {
		if !p.Enabled || p.Key == "" || existing[p.Key] {
			continue
		}
		sep := "?"
		if hasQ {
			sep = "&"
		}
		raw += sep + url.QueryEscape(p.Key) + "=" + url.QueryEscape(p.Value)
		hasQ = true
		existing[p.Key] = true
	}
	if strings.EqualFold(req.Auth.Type, "apikey") && strings.EqualFold(req.Auth.Params["in"], "query") {
		key, value := req.Auth.Params["key"], req.Auth.Params["value"]
		if key != "" && !existing[key] {
			sep := "?"
			if hasQ {
				sep = "&"
			}
			raw += sep + url.QueryEscape(key) + "=" + url.QueryEscape(value)
		}
	}
	return raw
}

// enabledHeaders 启用的 header（含 auth 的 Authorization 预览）
func enabledHeaders(req model.HttpRequest) []model.KV {
	var out []model.KV
	for _, h := range req.Headers {
		if h.Enabled && h.Key != "" {
			out = append(out, h)
		}
	}
	switch req.Auth.Type {
	case "bearer":
		out = append(out, model.KV{Key: "Authorization", Value: "Bearer " + req.Auth.Params["token"]})
	case "apikey":
		key, value := req.Auth.Params["key"], req.Auth.Params["value"]
		switch strings.ToLower(req.Auth.Params["in"]) {
		case "query":
			// query 形式已由 fullUrl 合并。
		case "cookie":
			if key != "" {
				out = append(out, model.KV{Key: "Cookie", Value: key + "=" + value})
			}
		default:
			if key != "" {
				out = append(out, model.KV{Key: key, Value: value})
			}
		}
	}
	return out
}

// bodyText 提取文本形式的 body（raw/urlencoded/graphql）；无则 ok=false
func bodyText(req model.HttpRequest) (text, contentType string, ok bool) {
	switch req.Body.Kind {
	case "raw":
		ct := map[string]string{
			"json": "application/json", "xml": "application/xml",
			"html": "text/html", "text": "text/plain",
		}[req.Body.Language]
		return req.Body.Text, ct, true
	case "urlencoded":
		var parts []string
		for _, it := range req.Body.Items {
			if it.Enabled && it.Key != "" {
				parts = append(parts, url.QueryEscape(it.Key)+"="+url.QueryEscape(it.Value))
			}
		}
		return strings.Join(parts, "&"), "application/x-www-form-urlencoded", true
	case "graphql":
		vars := strings.TrimSpace(req.Body.Variables)
		if vars == "" {
			vars = "{}"
		}
		return fmt.Sprintf(`{"query":%s,"variables":%s}`, jsonQuote(req.Body.Query), vars),
			"application/json", true
	}
	return "", "", false
}

func jsonQuote(s string) string {
	b := strings.Builder{}
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
