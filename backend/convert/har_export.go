// HAR 1.2 导出（docs/interop.md）：与 harImporter 对称的导出方向。
// 集合树按序展开为 entries；HAR 规范要求 response 字段存在，导出为 0 占位
// （导出的是请求定义而非已捕获的会话）。
package convert

import (
	"encoding/json"
	"net/url"
	"sort"
	"time"

	"apirequest/backend/model"
)

type harNameValuePair struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type harPostData struct {
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

type harRequest struct {
	Method      string             `json:"method"`
	Url         string             `json:"url"`
	HTTPVersion string             `json:"httpVersion"`
	Headers     []harNameValuePair `json:"headers"`
	QueryString []harNameValuePair `json:"queryString"`
	Cookies     []harNameValuePair `json:"cookies"`
	HeadersSize int                `json:"headersSize"`
	BodySize    int                `json:"bodySize"`
	PostData    *harPostData       `json:"postData,omitempty"`
}

type harResponse struct {
	Status      int                `json:"status"`
	StatusText  string             `json:"statusText"`
	HTTPVersion string             `json:"httpVersion"`
	Headers     []harNameValuePair `json:"headers"`
	Cookies     []harNameValuePair `json:"cookies"`
	Content     struct {
		Size     int    `json:"size"`
		MimeType string `json:"mimeType"`
	} `json:"content"`
	RedirectURL string `json:"redirectURL"`
}

type harEntry struct {
	StartedDateTime string      `json:"startedDateTime"`
	Time            float64     `json:"time"`
	Request         harRequest  `json:"request"`
	Response        harResponse `json:"response"`
}

type harCreator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type harLog struct {
	Version string     `json:"version"`
	Creator harCreator `json:"creator"`
	Entries []harEntry `json:"entries"`
}

type harExporter struct{}

func (harExporter) Format() string { return "har" }

// Export 把集合树序列化为 HAR 1.2 JSON
func (harExporter) Export(collection model.Node, children []model.Node) (string, error) {
	byParent := map[string][]model.Node{}
	for _, c := range children {
		byParent[c.ParentId] = append(byParent[c.ParentId], c)
	}
	for _, siblings := range byParent {
		sort.Slice(siblings, func(i, j int) bool {
			return siblings[i].SortOrder < siblings[j].SortOrder
		})
	}
	log := harLog{
		Version: "1.2",
		Creator: harCreator{Name: "ApiRequest", Version: "1.0"},
		Entries: []harEntry{},
	}
	started := time.Now().UTC()
	var walk func(parentId string)
	walk = func(parentId string) {
		for _, n := range byParent[parentId] {
			if n.Kind == "folder" {
				walk(n.Id)
				continue
			}
			if n.Kind != "request" || n.Request == nil {
				continue
			}
			entry := harEntry{
				StartedDateTime: started.Format(time.RFC3339Nano),
				Request: harRequest{
					Method:      n.Request.Method,
					Url:         n.Request.Url,
					HTTPVersion: "HTTP/1.1",
					Headers:     []harNameValuePair{},
					QueryString: []harNameValuePair{},
					Cookies:     []harNameValuePair{},
				},
				Response: harResponse{
					Headers: []harNameValuePair{},
					Cookies: []harNameValuePair{},
				},
			}
			for _, h := range n.Request.Headers {
				if h.Enabled && h.Key != "" {
					entry.Request.Headers = append(entry.Request.Headers,
						harNameValuePair{Name: h.Key, Value: h.Value})
				}
			}
			if u, err := url.Parse(n.Request.Url); err == nil {
				for k, vs := range u.Query() {
					for _, v := range vs {
						entry.Request.QueryString = append(entry.Request.QueryString,
							harNameValuePair{Name: k, Value: v})
					}
				}
			}
			if text := n.Request.Body.Text; text != "" {
				entry.Request.PostData = &harPostData{
					MimeType: bodyMimeType(n.Request.Body),
					Text:     text,
				}
				entry.Request.BodySize = len(text)
			}
			log.Entries = append(log.Entries, entry)
		}
	}
	walk(collection.Id)
	out, err := json.MarshalIndent(struct {
		Log harLog `json:"log"`
	}{Log: log}, "", "  ")
	if err != nil {
		return "", model.WrapError(model.KindImport, err)
	}
	return string(out), nil
}

// bodyMimeType 从 body 形态推断 mimeType（raw 按语言；form/urlencoded 有约定值）
func bodyMimeType(body model.Body) string {
	switch body.Kind {
	case "raw":
		switch body.Language {
		case "json":
			return "application/json"
		case "xml":
			return "application/xml"
		case "html":
			return "text/html"
		default:
			return "text/plain"
		}
	case "urlencoded":
		return "application/x-www-form-urlencoded"
	case "formdata":
		return "multipart/form-data"
	default:
		return ""
	}
}

func init() { RegisterExporter(harExporter{}) }
