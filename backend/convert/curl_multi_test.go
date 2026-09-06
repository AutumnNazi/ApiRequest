package convert

import (
	"testing"
)

// cURL 多命令导入：bash 历史整段粘贴（每行一条 curl）→ 每条一个请求
func TestCurlImportMultipleCommands(t *testing.T) {
	payload := `curl 'https://api.test/users'
curl -X POST 'https://api.test/users' -H 'Content-Type: application/json' -d '{"n":1}'
curl 'https://api.test/items' -H 'X-Api: k'`
	res, err := Import("curl", payload)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(res.Children) != 3 {
		t.Fatalf("children = %d, want 3: %+v", len(res.Children), res.Children)
	}
	if res.Children[0].Request.Method != "GET" {
		t.Fatalf("first command method = %q, want GET", res.Children[0].Request.Method)
	}
	// 第二条：POST + body
	var post *struct{}
	_ = post
	for _, n := range res.Children {
		if n.Request != nil && n.Request.Method == "POST" {
			if n.Request.Body.Kind != "raw" || n.Request.Body.Text != `{"n":1}` {
				t.Fatalf("POST body wrong: %+v", n.Request.Body)
			}
			if len(n.Request.Headers) != 1 || n.Request.Headers[0].Key != "Content-Type" {
				t.Fatalf("POST headers wrong: %+v", n.Request.Headers)
			}
		}
	}
	// 第三条：GET + header
	for _, n := range res.Children {
		if n.Request != nil && n.Request.Method == "GET" && len(n.Request.Headers) == 1 {
			if n.Request.Headers[0].Key != "X-Api" {
				t.Fatalf("third command header wrong: %+v", n.Request.Headers)
			}
		}
	}
}

// 多命令 + 续行反斜杠：行内的 \ 换行继续属于同一条命令
func TestCurlImportMultipleWithLineContinuation(t *testing.T) {
	payload := "curl 'https://a.test/1' \\\n  -H 'K: v'\ncurl 'https://a.test/2'"
	res, err := Import("curl", payload)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(res.Children) != 2 {
		t.Fatalf("children = %d, want 2", len(res.Children))
	}
	if len(res.Children[0].Request.Headers) != 1 || res.Children[0].Request.Headers[0].Key != "K" {
		t.Fatalf("continuation header lost: %+v", res.Children[0].Request.Headers)
	}
}

// 单命令（既有行为）不受影响：注释行与空行被忽略
func TestCurlImportMultipleIgnoresNoise(t *testing.T) {
	payload := "# a comment\ncurl 'https://a.test/x'\n\n# another\ncurl 'https://a.test/y'"
	res, err := Import("curl", payload)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(res.Children) != 2 {
		t.Fatalf("children = %d, want 2 (comments skipped)", len(res.Children))
	}
}
