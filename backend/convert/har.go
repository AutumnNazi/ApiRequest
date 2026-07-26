package convert

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"apirequest/backend/model"
)

// HAR 导入（docs/interop.md §2.4）：entries 逐条转 request，同 host 归一 folder

type harImporter struct{}

func (harImporter) Format() string { return "har" }

func (harImporter) Detect(payload string) bool {
	return strings.Contains(payload, `"log"`) && strings.Contains(payload, `"entries"`)
}

type harFile struct {
	Log struct {
		Entries []struct {
			Request struct {
				Method  string `json:"method"`
				Url     string `json:"url"`
				Headers []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
				} `json:"headers"`
				QueryString []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
				} `json:"queryString"`
				PostData *struct {
					MimeType string `json:"mimeType"`
					Text     string `json:"text"`
				} `json:"postData"`
			} `json:"request"`
		} `json:"entries"`
	} `json:"log"`
}

func (harImporter) Import(payload string) (*ImportResult, error) {
	var har harFile
	if err := json.Unmarshal([]byte(payload), &har); err != nil {
		return nil, &model.AppError{Kind: model.KindImport, Format: "har",
			Detail: "invalid HAR JSON: " + err.Error()}
	}
	if len(har.Log.Entries) == 0 {
		return nil, &model.AppError{Kind: model.KindImport, Format: "har",
			Detail: "HAR has no entries"}
	}

	res := &ImportResult{
		Collection: model.Node{Id: "import-root", Kind: "collection", Name: "Imported from HAR"},
	}
	folderId := map[string]string{}
	nodeSeq := 0
	order := 0.0
	nextId := func() string {
		nodeSeq++
		return fmt.Sprintf("import-%d", nodeSeq)
	}

	skipHeader := map[string]bool{
		// 浏览器自动生成的头，回放时应由引擎重算
		"content-length": true, "host": true, "connection": true,
		"accept-encoding": true, "cookie": true, // cookie 由 Jar 管理
	}

	for _, entry := range har.Log.Entries {
		r := entry.Request
		u, err := url.Parse(r.Url)
		if err != nil {
			res.Warnings = append(res.Warnings, "skipped invalid url: "+r.Url)
			continue
		}
		host := u.Host
		parent, ok := folderId[host]
		if !ok {
			order += 10
			f := model.Node{
				Id: nextId(), ParentId: res.Collection.Id, Kind: "folder",
				Name: host, SortOrder: order,
			}
			res.Children = append(res.Children, f)
			folderId[host] = f.Id
			parent = f.Id
		}

		req := model.HttpRequest{
			Method:   strings.ToUpper(r.Method),
			Url:      u.Scheme + "://" + u.Host + u.Path,
			Params:   []model.KV{},
			Headers:  []model.KV{},
			Body:     model.Body{Kind: "none"},
			Auth:     model.Auth{Type: "none"},
			Settings: model.DefaultSettings(),
		}
		for _, q := range r.QueryString {
			req.Params = append(req.Params, model.KV{Key: q.Name, Value: q.Value, Enabled: true})
		}
		for _, h := range r.Headers {
			if strings.HasPrefix(h.Name, ":") || skipHeader[strings.ToLower(h.Name)] {
				continue // HTTP/2 伪头与自动头跳过
			}
			req.Headers = append(req.Headers, model.KV{Key: h.Name, Value: h.Value, Enabled: true})
		}
		if r.PostData != nil && r.PostData.Text != "" {
			if strings.Contains(r.PostData.MimeType, "json") {
				req.Body = model.Body{Kind: "raw", Language: "json", Text: r.PostData.Text}
			} else if strings.Contains(r.PostData.MimeType, "x-www-form-urlencoded") {
				items := []model.FormItem{}
				if form, err := url.ParseQuery(r.PostData.Text); err == nil {
					for k, vs := range form {
						for _, v := range vs {
							items = append(items, model.FormItem{Key: k, Value: v, Type: "text", Enabled: true})
						}
					}
				}
				req.Body = model.Body{Kind: "urlencoded", Items: items}
			} else {
				req.Body = model.Body{Kind: "raw", Language: "text", Text: r.PostData.Text}
			}
		}

		name := req.Method + " " + u.Path
		if u.Path == "" || u.Path == "/" {
			name = req.Method + " /"
		}
		order += 10
		res.Children = append(res.Children, model.Node{
			Id: nextId(), ParentId: parent, Kind: "request",
			Name: name, SortOrder: order, Request: &req,
		})
	}
	return res, nil
}

func init() { RegisterImporter(harImporter{}) }
