package codegen

import (
	"strings"

	"apirequest/backend/model"
)

// ── HTTPie（Shell）──
// HTTPie 的语法核心：`http METHOD URL 头:值 --raw/--stdin`；
// JSON raw body 走 stdin（`<<< 'json'`），比 --raw 更贴近 CLI 交互用法。

type httpieGen struct{}

func (httpieGen) Id() string   { return "shell-httpie" }
func (httpieGen) Name() string { return "HTTPie" }

func (httpieGen) Generate(req model.HttpRequest) string {
	var b strings.Builder
	b.WriteString("http " + orGET(req.Method) + " '" + shellQuote(fullUrl(req)) + "'")
	// enabledHeaders 已把 bearer 摊平成 Authorization 头；HTTPie 走 --auth 更地道，
	// 该头去掉避免双份
	for _, h := range enabledHeaders(req) {
		if req.Auth.Type == "bearer" && strings.EqualFold(h.Key, "Authorization") {
			continue
		}
		b.WriteString(" \\\n  '" + shellQuote(h.Key+":"+h.Value) + "'")
	}
	switch req.Auth.Type {
	case "basic":
		cred := req.Auth.Params["username"] + ":" + req.Auth.Params["password"]
		b.WriteString(" \\\n  --auth-type=basic --auth='" + shellQuote(cred) + "'")
	case "bearer":
		b.WriteString(" \\\n  --auth-type=bearer --auth='" + shellQuote(req.Auth.Params["token"]) + "'")
	}
	switch req.Body.Kind {
	case "urlencoded":
		b.WriteString(" \\\n  --form")
		for _, it := range req.Body.Items {
			if !it.Enabled || it.Key == "" {
				continue
			}
			b.WriteString(" \\\n  '" + shellQuote(it.Key+"="+it.Value) + "'")
		}
	case "formdata":
		b.WriteString(" \\\n  --multipart")
		for _, it := range req.Body.Items {
			if !it.Enabled || it.Key == "" {
				continue
			}
			if it.Type == "file" {
				b.WriteString(" \\\n  '" + shellQuote(it.Key+"=@"+it.Path) + "'")
			} else {
				b.WriteString(" \\\n  '" + shellQuote(it.Key+"="+it.Value) + "'")
			}
		}
	case "binary":
		if req.Body.Path != "" {
			b.WriteString(" \\\n  --raw < '" + shellQuote(req.Body.Path) + "'")
		}
	default:
		if text, ct, ok := bodyText(req); ok && text != "" {
			// HTTPie 对 JSON/text 自动加 Content-Type；xml/html 显式补
			if ct != "" && !strings.HasPrefix(ct, "application/json") && !strings.HasPrefix(ct, "text/") && !hasHeader(req, "Content-Type") {
				b.WriteString(" \\\n  '" + shellQuote("Content-Type:"+ct) + "'")
			}
			b.WriteString(" \\\n  <<< '" + shellQuote(text) + "'")
		}
	}
	if !req.Settings.VerifyTLS {
		b.WriteString(" \\\n  --verify=no")
	}
	return b.String()
}

// ── JavaScript (axios) ──

type axiosGen struct{}

func (axiosGen) Id() string   { return "javascript-axios" }
func (axiosGen) Name() string { return "JavaScript (axios)" }

func (axiosGen) Generate(req model.HttpRequest) string {
	var b strings.Builder
	b.WriteString("const { data } = await axios({\n")
	b.WriteString("  url: " + jsonQuote(fullUrl(req)) + ",\n")
	b.WriteString("  method: " + jsonQuote(orGET(req.Method)) + ",\n")
	headers := enabledHeaders(req)
	// basic 走 axios 的 auth 选项（下方）；bearer 已由 enabledHeaders 摊平成 Authorization 头
	if _, ct, hasBody := bodyText(req); hasBody && ct != "" && !hasHeader(req, "Content-Type") {
		headers = append(headers, model.KV{Key: "Content-Type", Value: ct})
	}
	if len(headers) > 0 {
		b.WriteString("  headers: {\n")
		for _, h := range headers {
			b.WriteString("    " + jsonQuote(h.Key) + ": " + jsonQuote(h.Value) + ",\n")
		}
		b.WriteString("  },\n")
	}
	if text, _, hasBody := bodyText(req); hasBody && text != "" {
		b.WriteString("  data: " + jsonQuote(text) + ",\n")
	} else if req.Body.Kind == "formdata" {
		b.WriteString("  data: new FormData(),\n")
		for _, it := range req.Body.Items {
			if !it.Enabled || it.Key == "" {
				continue
			}
			if it.Type == "file" {
				b.WriteString("  // data.append(" + jsonQuote(it.Key) + ", file /* " + jsonQuote(it.Path) + " */);\n")
			} else {
				b.WriteString("  data.append(" + jsonQuote(it.Key) + ", " + jsonQuote(it.Value) + ");\n")
			}
		}
	}
	if req.Auth.Type == "basic" {
		b.WriteString("  auth: {\n    username: " + jsonQuote(req.Auth.Params["username"]) +
			",\n    password: " + jsonQuote(req.Auth.Params["password"]) + ",\n  },\n")
	}
	b.WriteString("});\nconsole.log(data);")
	return b.String()
}

func init() {
	Register(httpieGen{})
	Register(axiosGen{})
}
