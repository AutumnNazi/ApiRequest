package convert

import (
	"errors"
	"fmt"
	"strings"

	"apirequest/backend/model"
)

// cURL 命令导入（docs/interop.md §2.3）：解析常用 flag 生成单请求集合

type curlImporter struct{}

func (curlImporter) Format() string { return "curl" }

func (curlImporter) Detect(payload string) bool {
	return firstCurlLine(payload) != ""
}

// Import 支持多命令：bash 历史整段粘贴时按顶层 `curl` 行拆分，
// 每条命令一个请求；单命令行为不变
func (curlImporter) Import(payload string) (*ImportResult, error) {
	commands := splitCurlCommands(payload)
	if len(commands) == 0 {
		return nil, &model.AppError{Kind: model.KindImport, Format: "curl", Detail: "not a curl command"}
	}
	var parsed []*ImportResult
	for _, cmd := range commands {
		res, err := parseCurlCommand(cmd)
		if err != nil {
			return nil, err
		}
		parsed = append(parsed, res)
	}
	if len(parsed) == 1 {
		return parsed[0], nil
	}
	// 多命令：兄弟请求平铺（名字各自独立）
	out := &ImportResult{Collection: parsed[0].Collection}
	for _, p := range parsed {
		for i := range p.Children {
			p.Children[i].Id = fmt.Sprintf("import-%d", len(out.Children)+1)
			p.Children[i].SortOrder = float64((len(out.Children) + 1) * 10)
			out.Children = append(out.Children, p.Children[i])
		}
		out.Warnings = append(out.Warnings, p.Warnings...)
	}
	return out, nil
}

// splitCurlCommands 把 payload 切成独立的 curl 命令文本：
// 1) 行尾 `\` 续行合并（属于同一条命令）；
// 2) 注释行与空行跳过；
// 3) 以 `curl ` 开头的新行 = 新命令的起点
func splitCurlCommands(payload string) []string {
	lines := strings.Split(payload, "\n")
	// 逻辑行：先把续行拼回去
	var logical []string
	for _, raw := range lines {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if strings.HasSuffix(trimmed, "\\") && !strings.HasSuffix(trimmed, "\\\\") {
			logical = append(logical, strings.TrimSuffix(trimmed, "\\"))
			continue
		}
		logical = append(logical, trimmed)
	}
	var commands []string
	var current []string
	flush := func() {
		joined := strings.TrimSpace(strings.Join(current, "\n"))
		if joined != "" {
			commands = append(commands, joined)
		}
		current = nil
	}
	for _, line := range logical {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "curl ") || line == "curl" {
			flush()
		}
		current = append(current, line)
	}
	flush()
	return commands
}

// firstCurlLine Detect 用：首个 curl 行存在即认
func firstCurlLine(payload string) string {
	for _, line := range strings.Split(payload, "\n") {
		t := strings.TrimSpace(strings.TrimRight(line, "\r"))
		if strings.HasPrefix(t, "curl ") || t == "curl" {
			return t
		}
	}
	return ""
}

// parseCurlCommand 解析单条 curl 命令（原 Import 主体）
func parseCurlCommand(command string) (*ImportResult, error) {
	tokens, err := tokenize(command)
	if err != nil {
		return nil, &model.AppError{Kind: model.KindImport, Format: "curl", Detail: err.Error()}
	}
	if len(tokens) == 0 || tokens[0] != "curl" {
		return nil, &model.AppError{Kind: model.KindImport, Format: "curl", Detail: "not a curl command"}
	}

	req := model.HttpRequest{
		Method:   "",
		Params:   []model.KV{},
		Headers:  []model.KV{},
		Body:     model.Body{Kind: "none"},
		Auth:     model.Auth{Type: "none"},
		Settings: model.DefaultSettings(),
	}
	res := &ImportResult{}
	var dataParts []string
	var formItems []model.FormItem

	next := func(i *int) string {
		*i++
		if *i < len(tokens) {
			return tokens[*i]
		}
		return ""
	}

	for i := 1; i < len(tokens); i++ {
		tok := tokens[i]
		switch {
		case tok == "-X" || tok == "--request":
			req.Method = strings.ToUpper(next(&i))
		case tok == "-H" || tok == "--header":
			h := next(&i)
			if kv := strings.SplitN(h, ":", 2); len(kv) == 2 {
				req.Headers = append(req.Headers, model.KV{
					Key: strings.TrimSpace(kv[0]), Value: strings.TrimSpace(kv[1]), Enabled: true,
				})
			}
		case tok == "-d" || tok == "--data" || tok == "--data-raw" || tok == "--data-binary" || tok == "--data-ascii":
			dataParts = append(dataParts, next(&i))
		case tok == "--data-urlencode":
			dataParts = append(dataParts, next(&i))
		case tok == "-F" || tok == "--form":
			f := next(&i)
			if kv := strings.SplitN(f, "=", 2); len(kv) == 2 {
				it := model.FormItem{Key: kv[0], Type: "text", Value: kv[1], Enabled: true}
				if strings.HasPrefix(kv[1], "@") {
					it.Type = "file"
					it.Path = kv[1][1:]
					it.Value = ""
				}
				formItems = append(formItems, it)
			}
		case tok == "-u" || tok == "--user":
			cred := next(&i)
			parts := strings.SplitN(cred, ":", 2)
			p := map[string]string{"username": parts[0]}
			if len(parts) == 2 {
				p["password"] = parts[1]
			}
			req.Auth = model.Auth{Type: "basic", Params: p}
		case tok == "-b" || tok == "--cookie":
			req.Headers = append(req.Headers, model.KV{Key: "Cookie", Value: next(&i), Enabled: true})
		case tok == "-A" || tok == "--user-agent":
			req.Headers = append(req.Headers, model.KV{Key: "User-Agent", Value: next(&i), Enabled: true})
		case tok == "-e" || tok == "--referer":
			req.Headers = append(req.Headers, model.KV{Key: "Referer", Value: next(&i), Enabled: true})
		case tok == "-k" || tok == "--insecure":
			req.Settings.VerifyTLS = false
		case tok == "-L" || tok == "--location":
			req.Settings.FollowRedirects = true
		case tok == "--compressed":
			// 引擎自动解压，忽略
		case tok == "--url":
			req.Url = next(&i)
		case tok == "-o" || tok == "--output" || tok == "-s" || tok == "--silent" ||
			tok == "-v" || tok == "--verbose" || tok == "-i" || tok == "--include":
			if tok == "-o" || tok == "--output" {
				next(&i) // 吞掉参数
			}
		case strings.HasPrefix(tok, "-"):
			res.Warnings = append(res.Warnings, "ignored flag: "+tok)
			// 未知 flag：可能带参，保守不吞
		default:
			if req.Url == "" {
				req.Url = tok
			}
		}
	}

	// body 组装（interop.md：-d 多次合并；json Content-Type → raw(json)，否则 urlencoded）
	if len(formItems) > 0 {
		req.Body = model.Body{Kind: "formdata", Items: formItems}
	} else if len(dataParts) > 0 {
		joined := strings.Join(dataParts, "&")
		if headerContains(req.Headers, "Content-Type", "json") {
			req.Body = model.Body{Kind: "raw", Language: "json", Text: joined}
		} else if len(dataParts) == 1 && (strings.HasPrefix(strings.TrimSpace(joined), "{") || strings.HasPrefix(strings.TrimSpace(joined), "[")) {
			req.Body = model.Body{Kind: "raw", Language: "json", Text: joined}
		} else {
			items := []model.FormItem{}
			for _, part := range strings.Split(joined, "&") {
				if kv := strings.SplitN(part, "=", 2); len(kv) == 2 {
					items = append(items, model.FormItem{Key: kv[0], Value: kv[1], Type: "text", Enabled: true})
				}
			}
			req.Body = model.Body{Kind: "urlencoded", Items: items}
		}
	}
	if req.Method == "" {
		if req.Body.Kind != "none" {
			req.Method = "POST"
		} else {
			req.Method = "GET"
		}
	}

	name := req.Method + " " + req.Url
	res.Collection = model.Node{Id: "import-root", Kind: "collection", Name: "Imported from cURL"}
	res.Children = []model.Node{{
		Id: "import-1", ParentId: "import-root", Kind: "request",
		Name: name, SortOrder: 10, Request: &req,
	}}
	return res, nil
}

func headerContains(headers []model.KV, key, substr string) bool {
	for _, h := range headers {
		if strings.EqualFold(h.Key, key) && strings.Contains(strings.ToLower(h.Value), substr) {
			return true
		}
	}
	return false
}

// tokenize 按 shell 规则分词：支持单双引号、反斜杠续行、$'...' 简化处理
func tokenize(s string) ([]string, error) {
	s = strings.ReplaceAll(s, "\\\n", " ")
	s = strings.ReplaceAll(s, "^\n", " ") // Windows cmd 续行
	var tokens []string
	var cur strings.Builder
	var quote rune
	esc := false
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case esc:
			cur.WriteRune(r)
			esc = false
		case r == '\\' && quote != '\'':
			esc = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t' || r == '\n' || r == '\r':
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	if quote != 0 {
		return nil, errUnclosedQuote
	}
	flush()
	return tokens, nil
}

var errUnclosedQuote = errors.New("unclosed quote in curl command")

func init() { RegisterImporter(curlImporter{}) }
