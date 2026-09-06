// REST Client (.http) 格式：VS Code REST Client / JetBrains HTTP client 通用的
// 纯文本请求集（docs/interop.md）。导入按 "###" 或方法行分块；导出按树序展开。
package convert

import (
	"fmt"
	"sort"
	"strings"

	"apirequest/backend/model"
)

type restClientImporter struct{}

func (restClientImporter) Format() string { return "restclient" }

var restClientMethods = map[string]bool{
	"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true,
	"HEAD": true, "OPTIONS": true,
}

func (restClientImporter) Detect(payload string) bool {
	for _, line := range strings.Split(payload, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		fields := strings.Fields(line)
		return len(fields) >= 2 && restClientMethods[strings.ToUpper(fields[0])]
	}
	return false
}

// isHeaderLine 判定是否 header 行：冒号前必须是合法 header token
//（RFC 9110 tchar：不含空格与分隔符，排除 JSON body 被误判）
func isHeaderLine(line string) bool {
	idx := strings.Index(line, ":")
	if idx <= 0 {
		return false
	}
	name := line[:idx]
	if strings.ContainsAny(name, " {}[]()<>@,;\"'") {
		return false
	}
	for _, ch := range name {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9':
		case strings.ContainsRune("!#$%&'*+-.^_`|~", ch):
		default:
			return false
		}
	}
	return true
}

func (restClientImporter) Import(payload string) (*ImportResult, error) {
	res := &ImportResult{
		Collection: model.Node{Id: "import-root", Kind: "collection", Name: "Imported from .http"},
	}
	type block struct {
		name  string
		lines []string
	}
	var blocks []block
	var current *block
	// 无 "###" 标题的单块文件：整段作为一个请求
	for _, raw := range strings.Split(payload, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "###") {
			blocks = append(blocks, block{name: strings.TrimSpace(strings.TrimPrefix(trimmed, "###"))})
			current = &blocks[len(blocks)-1]
			continue
		}
		if trimmed == "" {
			if current != nil {
				current.lines = append(current.lines, "")
			}
			continue
		}
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
			continue // 注释行
		}
		fields := strings.Fields(trimmed)
		if current == nil || (restClientMethods[strings.ToUpper(fields[0])] && len(current.lines) > 0) {
			// 方法行开启新请求（兼容无 ### 分隔的多请求文件）
			blocks = append(blocks, block{})
			current = &blocks[len(blocks)-1]
		}
		if current.name == "" {
			current.name = trimmed
		}
		current.lines = append(current.lines, trimmed)
	}

	sortOrder := float64(10)
	for _, b := range blocks {
		req, name, err := parseRestClientBlock(b.lines)
		if err != nil {
			return nil, model.NewError(model.KindImport, err.Error())
		}
		if name == "" {
			name = b.name
		}
		if req == nil {
			continue
		}
		res.Children = append(res.Children, model.Node{
			Id: fmt.Sprintf("import-%d", len(res.Children)+1), ParentId: "import-root",
			Kind: "request", Name: name, SortOrder: sortOrder, Request: req,
		})
		sortOrder += 10
	}
	if len(res.Children) == 0 {
		return nil, model.NewError(model.KindImport, "no requests found in .http payload")
	}
	return res, nil
}

// parseRestClientBlock 解析单个请求块：方法行 + header 行 + 空行后的 body
func parseRestClientBlock(lines []string) (*model.HttpRequest, string, error) {
	var req *model.HttpRequest
	var name string
	var bodyLines []string
	inBody := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		fields := strings.Fields(trimmed)
		if req == nil {
			if len(fields) < 2 || !restClientMethods[strings.ToUpper(fields[0])] {
				// 首个非空行可能是 name 变量（@name=value）或无效内容
				if strings.HasPrefix(trimmed, "@") {
					if kv := strings.SplitN(strings.TrimPrefix(trimmed, "@"), "=", 2); len(kv) == 2 {
						name = strings.TrimSpace(kv[1])
						continue
					}
				}
				continue
			}
			req = &model.HttpRequest{
				Method: strings.ToUpper(fields[0]), Url: fields[1],
				Auth: model.Auth{Type: "none"}, Settings: model.DefaultSettings(),
			}
			continue
		}
		if !inBody && isHeaderLine(trimmed) {
			if kv := strings.SplitN(trimmed, ":", 2); len(kv) == 2 {
				req.Headers = append(req.Headers, model.KV{
					Key: strings.TrimSpace(kv[0]), Value: strings.TrimSpace(kv[1]), Enabled: true,
				})
				continue
			}
		}
		if trimmed == "" {
			if len(bodyLines) > 0 {
				inBody = true
			}
			continue
		}
		bodyLines = append(bodyLines, trimmed)
	}
	if req == nil {
		return nil, "", nil
	}
	if len(bodyLines) > 0 {
		req.Body = model.Body{Kind: "raw", Text: strings.Join(bodyLines, "\n")}
	}
	return req, name, nil
}

type restClientExporter struct{}

func (restClientExporter) Format() string { return "restclient" }

// Export 把集合树序列化为 .http：请求按树序，body 以空行分隔
func (restClientExporter) Export(collection model.Node, children []model.Node) (string, error) {
	byParent := map[string][]model.Node{}
	for _, c := range children {
		byParent[c.ParentId] = append(byParent[c.ParentId], c)
	}
	for _, siblings := range byParent {
		sort.Slice(siblings, func(i, j int) bool {
			return siblings[i].SortOrder < siblings[j].SortOrder
		})
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n", collection.Name)
	var walk func(parentId string)
	walk = func(parentId string) {
		for _, n := range byParent[parentId] {
			if n.Kind == "folder" {
				fmt.Fprintf(&sb, "\n## %s\n", n.Name)
				walk(n.Id)
				continue
			}
			if n.Kind != "request" || n.Request == nil {
				continue
			}
			fmt.Fprintf(&sb, "\n### %s\n", n.Name)
			fmt.Fprintf(&sb, "%s %s\n", n.Request.Method, n.Request.Url)
			for _, h := range n.Request.Headers {
				if h.Enabled && h.Key != "" {
					fmt.Fprintf(&sb, "%s: %s\n", h.Key, h.Value)
				}
			}
			if text := strings.TrimSpace(n.Request.Body.Text); text != "" {
				fmt.Fprintf(&sb, "\n%s\n", text)
			}
		}
	}
	walk(collection.Id)
	return sb.String(), nil
}

func init() {
	RegisterImporter(restClientImporter{})
	RegisterExporter(restClientExporter{})
}
