package convert

import (
	"fmt"
	"strings"

	"apirequest/backend/model"
)

// Bruno .bru 纯文本格式导入（docs/interop.md）：单文件 = 单请求。
// 语法要点：块以 `name {` 开头、独立 `}` 结尾（body:json 内允许嵌套大括号，
// 用计数配平）；params/headers 行的 `~` 前缀表示禁用；script:pre/post 映射
// 前置/测试脚本；vars:pre（请求级变量）暂无落点，计入 Warnings。
// Bruno 集合是目录树，单文件导入场景见 ImportDialog（目录级导入待需求出现再做）。

type brunoImporter struct{}

func (brunoImporter) Format() string { return "bruno" }

func (brunoImporter) Detect(payload string) bool {
	return brunoHasBlock(payload, "meta") && brunoHasBlock(payload, "url:")
}

// brunoHasBlock 粗判是否存在指定块（独立行 `xxx {` 或键值行 `xxx:`）
func brunoHasBlock(payload, marker string) bool {
	for _, line := range strings.Split(payload, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, marker) {
			return true
		}
	}
	return false
}

func (brunoImporter) Import(payload string) (*ImportResult, error) {
	// 逐块解析：block 名 → 行列表。body:json/text 用大括号配平收集原样文本
	type brunoBlock struct {
		name  string
		lines []string
	}
	var blocks []brunoBlock
	var current *brunoBlock
	depth := 0
	for _, raw := range strings.Split(payload, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if current != nil && depth > 0 {
			for _, ch := range trimmed {
				switch ch {
				case '{':
					depth++
				case '}':
					depth--
				}
			}
			if depth <= 0 && trimmed == "}" {
				current = nil
				continue
			}
			current.lines = append(current.lines, trimmed)
			continue
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
			continue
		}
		if strings.HasSuffix(trimmed, "{") {
			name := strings.TrimSpace(strings.TrimSuffix(trimmed, "{"))
			blocks = append(blocks, brunoBlock{name: name})
			current = &blocks[len(blocks)-1]
			depth = 1
		}
	}
	if len(blocks) == 0 {
		return nil, model.NewError(model.KindImport, "no blocks found in .bru payload")
	}

	req := model.HttpRequest{Settings: model.DefaultSettings(), Auth: model.Auth{Type: "none"}}
	name := ""
	var warnings []string
	var preScript, testScript strings.Builder
	// 请求级变量（vars:pre）：当前导入模型无落点，收集进 Warnings
	var varLines []string

	for _, b := range blocks {
		switch {
		case b.name == "meta":
			for _, kv := range parseBrunoLines(b.lines) {
				if kv.key == "name" {
					name = brunoUnquote(kv.value)
				}
			}
		case isBrunoMethodBlock(b.name):
			for _, kv := range parseBrunoLines(b.lines) {
				if kv.key == "url" {
					req.Url = brunoUnquote(kv.value)
				}
			}
			// params/headers/auth 在方法块内是子块（Bruno v1.10+ 形态）
			req.Params = append(req.Params, brunoSubPairs(b.lines, "params", "headers")...)
			req.Headers = append(req.Headers, brunoSubPairs(b.lines, "headers", "params")...)
			if auth := brunoSubAuth(b.lines); auth != nil {
				req.Auth = *auth
			}
		case b.name == "body:json":
			req.Body = model.Body{Kind: "raw", Language: "json", Text: brunoBodyText(b.lines)}
		case b.name == "body:text":
			req.Body = model.Body{Kind: "raw", Language: "text", Text: brunoBodyText(b.lines)}
		case b.name == "body:xml":
			req.Body = model.Body{Kind: "raw", Language: "xml", Text: brunoBodyText(b.lines)}
		case b.name == "body:formUrlEncoded":
			req.Body = model.Body{Kind: "urlencoded", Items: brunoFormItems(b.lines)}
		case b.name == "body:multipart":
			req.Body = model.Body{Kind: "formdata", Items: brunoFormItems(b.lines)}
		case b.name == "auth:bearer":
			for _, kv := range parseBrunoLines(b.lines) {
				if kv.key == "token" {
					req.Auth = model.Auth{Type: "bearer", Params: map[string]string{"token": brunoUnquote(kv.value)}}
				}
			}
		case b.name == "auth:basic":
			auth := model.Auth{Type: "basic", Params: map[string]string{}}
			for _, kv := range parseBrunoLines(b.lines) {
				switch kv.key {
				case "username":
					auth.Params["username"] = brunoUnquote(kv.value)
				case "password":
					auth.Params["password"] = brunoUnquote(kv.value)
				}
			}
			req.Auth = auth
		case b.name == "script:pre":
			for _, l := range b.lines {
				preScript.WriteString(strings.TrimPrefix(l, "") + "\n")
			}
		case b.name == "script:post" || b.name == "test":
			for _, l := range b.lines {
				testScript.WriteString(l + "\n")
			}
		case b.name == "vars:pre" || b.name == "vars:post":
			varLines = append(varLines, b.lines...)
		default:
			warnings = append(warnings, fmt.Sprintf("bruno block %q was skipped", b.name))
		}
	}
	// method 块名即请求方法（get { / post { ...）
	for _, b := range blocks {
		if isBrunoMethodBlock(b.name) {
			req.Method = strings.ToUpper(b.name)
			break
		}
	}
	if len(varLines) > 0 {
		warnings = append(warnings, fmt.Sprintf("%d request-level variable line(s) (vars:*) were skipped", len(varLines)))
	}
	if req.Url == "" {
		return nil, model.NewError(model.KindImport, "no url found in .bru payload")
	}
	if name == "" {
		name = "Imported from .bru"
	}
	req.PreScript = strings.TrimSpace(preScript.String())
	req.TestScript = strings.TrimSpace(testScript.String())

	return &ImportResult{
		Collection: model.Node{Id: "import-root", Kind: "collection", Name: name},
		Children: []model.Node{{
			Id: "import-1", ParentId: "import-root", Kind: "request",
			Name: name, SortOrder: 10, Request: &req,
		}},
		Warnings: warnings,
	}, nil
}

func isBrunoMethodBlock(name string) bool {
	switch strings.ToLower(name) {
	case "get", "post", "put", "patch", "delete", "head", "options", "connect", "trace":
		return true
	}
	return false
}

type brunoKV struct {
	key      string
	value    string
	disabled bool
}

// parseBrunoLines 解析 `key: value` 行（~ 前缀 = 禁用）
func parseBrunoLines(lines []string) []brunoKV {
	var out []brunoKV
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || trimmed == "{" || trimmed == "}" {
			continue
		}
		disabled := strings.HasPrefix(trimmed, "~")
		if disabled {
			trimmed = strings.TrimPrefix(trimmed, "~")
		}
		idx := strings.Index(trimmed, ":")
		if idx <= 0 {
			continue
		}
		out = append(out, brunoKV{
			key:      strings.TrimSpace(trimmed[:idx]),
			value:    strings.TrimSpace(trimmed[idx+1:]),
			disabled: disabled,
		})
	}
	return out
}

// brunoSubPairs 从方法块行里提取 `params {` / `headers {` 子块的键值对
// （Bruno v1.10+ 把 params/headers 写成方法块内的子块）。
// excluded 用于同块双写时的排他（先到先得）。
func brunoSubPairs(lines []string, want, _ string) []model.KV {
	var out []model.KV
	inBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inBlock {
			if trimmed == want+" {" || (strings.HasPrefix(trimmed, want+":") && strings.HasSuffix(trimmed, "{")) {
				inBlock = true
			}
			continue
		}
		if trimmed == "}" {
			inBlock = false
			continue
		}
		disabled := strings.HasPrefix(trimmed, "~")
		body := strings.TrimPrefix(trimmed, "~")
		idx := strings.Index(body, ":")
		if idx <= 0 {
			continue
		}
		out = append(out, model.KV{
			Key:     strings.TrimSpace(body[:idx]),
			Value:   brunoUnquote(strings.TrimSpace(body[idx+1:])),
			Enabled: !disabled,
		})
	}
	return out
}

// brunoBodyText body 块的原样文本（去掉外层包裹与首尾空行）
func brunoBodyText(lines []string) string {
	var out []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "{" || trimmed == "}" {
			continue
		}
		out = append(out, line)
	}
	return strings.Trim(strings.Join(out, "\n"), "\n")
}

// brunoFormItems k: v 行转为表单项
func brunoFormItems(lines []string) []model.FormItem {
	var out []model.FormItem
	for _, kv := range parseBrunoLines(lines) {
		if kv.key == "" || kv.disabled {
			continue
		}
		out = append(out, model.FormItem{Key: kv.key, Value: brunoUnquote(kv.value), Type: "text", Enabled: true})
	}
	return out
}

// brunoUnquote 去掉值两侧的引号（Bruno 允许值带引号）
func brunoUnquote(v string) string {
	if len(v) >= 2 && (v[0] == '\'' && v[len(v)-1] == '\'' || v[0] == '"' && v[len(v)-1] == '"') {
		return v[1 : len(v)-1]
	}
	return v
}

func init() { RegisterImporter(brunoImporter{}) }

// brunoSubAuth 提取方法块内的 auth 子块（auth: bearer { / auth: basic { / auth { ）。
// 不支持的 auth 类型返回 nil 并由调用方保持默认（none）。
func brunoSubAuth(lines []string) *model.Auth {
	inBlock := false
	kind := ""
	var kvLines []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !inBlock {
			if strings.HasPrefix(trimmed, "auth") && strings.HasSuffix(trimmed, "{") {
				inBlock = true
				body := strings.TrimSuffix(strings.TrimPrefix(trimmed, "auth"), "{")
				kind = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(body), ":"))
			}
			continue
		}
		if trimmed == "}" {
			inBlock = false
			continue
		}
		kvLines = append(kvLines, trimmed)
	}
	if !inBlock && kind == "" && len(kvLines) == 0 {
		return nil
	}
	auth := &model.Auth{Type: "none", Params: map[string]string{}}
	switch strings.ToLower(kind) {
	case "bearer":
		auth.Type = "bearer"
		for _, kv := range parseBrunoLines(kvLines) {
			if kv.key == "token" {
				auth.Params["token"] = brunoUnquote(kv.value)
			}
		}
	case "basic":
		auth.Type = "basic"
		for _, kv := range parseBrunoLines(kvLines) {
			switch kv.key {
			case "username":
				auth.Params["username"] = brunoUnquote(kv.value)
			case "password":
				auth.Params["password"] = brunoUnquote(kv.value)
			}
		}
	default:
		// awsv1/awsv2/digest/wsse 等暂不支持
		return nil
	}
	if auth.Type == "none" {
		return nil
	}
	return auth
}
