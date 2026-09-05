// Package codegen 实现代码生成器（docs/interop.md §3）。
// IR(HttpRequest) → 目标语言片段；按 id 注册（docs/extensibility.md）。
package codegen

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"apirequest/backend/model"
	"apirequest/backend/requesturl"
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

// Targets 返回全部目标（按 id 排序）。
// 排序在副本上做：order 是包级可变状态，原地排序会在并发调用时数据竞争
func Targets() []Target {
	sorted := append([]string(nil), order...)
	sort.Strings(sorted)
	out := make([]Target, 0, len(sorted))
	for _, id := range sorted {
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
	raw := requesturl.AppendParams(req.Url, req.Params, true)
	if strings.EqualFold(req.Auth.Type, "apikey") && strings.EqualFold(req.Auth.Params["in"], "query") {
		key, value := req.Auth.Params["key"], req.Auth.Params["value"]
		raw = requesturl.SetParam(raw, key, value, true)
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

// controlEscapeForm 控制字符的目标语言转义形式。
// 必须在各 quote 函数的反斜杠翻倍/引号替换之后再套用：先插入转义序列
// 再翻倍会把 \u0001 变成 \\u0001（目标语言里的字面文本而非控制字符）。
type controlEscapeForm int

const (
	// formUnicode \uXXXX（JS/Java/Python/C#/JSON/Go 通用）
	formUnicode controlEscapeForm = iota
	// formRust 花括号形式 \u{1}——\u0001 在 Rust 里是语法错误
	formRust
	// formPhpHex 断链拼双引号段 "\x01"——PHP 单引号串没有 \u 转义，双引号串才有 \xHH
	formPhpHex
)

// escapeControlChars 把 \r \n \t 之外的控制字符按 form 转义。
// HTTP 头值可含任意控制字节，原样嵌进生成的代码会产出语法错误的片段；
// \r \n \t 有各语言的原生转义，由各 quote 函数自行处理，这里跳过。
// 只处理控制字符本身，不碰反斜杠与引号——所以必须在所有替换之后调用，
// 它插入的序列才不会被后续翻倍破坏。
func escapeControlChars(s string, form controlEscapeForm) string {
	if !strings.ContainsFunc(s, func(r rune) bool {
		return r < 0x20 && r != '\r' && r != '\n' && r != '\t'
	}) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 && r != '\r' && r != '\n' && r != '\t' {
			switch form {
			case formRust:
				fmt.Fprintf(&b, `\u{%x}`, r)
			case formPhpHex:
				fmt.Fprintf(&b, `'."\x%02x".'`, r)
			default:
				fmt.Fprintf(&b, `\u%04x`, r)
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
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
			if r < 0x20 {
				// 其余控制字符（HTTP 头值可含）按 \uXXXX 转义，否则生成非法 JS/JSON
				b.WriteString(fmt.Sprintf(`\u%04x`, r))
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
