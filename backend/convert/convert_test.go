package convert

import (
	"encoding/json"
	"strings"
	"testing"
)

const samplePostman = `{
  "info": {
    "_postman_id": "abc-123",
    "name": "Demo API",
    "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"
  },
  "variable": [{"key": "base", "value": "https://api.demo.io"}],
  "item": [
    {
      "name": "Users",
      "item": [
        {
          "name": "Get User",
          "event": [
            {"listen": "test", "script": {"exec": ["pm.test('ok', function () {", "  pm.expect(pm.response.code).to.equal(200);", "});"]}}
          ],
          "request": {
            "method": "GET",
            "url": {"raw": "{{base}}/users/1?full=true", "query": [{"key": "full", "value": "true"}]},
            "header": [{"key": "Accept", "value": "application/json"}],
            "auth": {"type": "bearer", "bearer": [{"key": "token", "value": "{{tok}}"}]}
          }
        },
        {
          "name": "Create User",
          "request": {
            "method": "POST",
            "url": "{{base}}/users",
            "header": [{"key": "Content-Type", "value": "application/json"}],
            "body": {
              "mode": "raw",
              "raw": "{\"name\":\"x\"}",
              "options": {"raw": {"language": "json"}}
            }
          }
        }
      ]
    }
  ]
}`

func TestPostmanImport(t *testing.T) {
	res, err := Import("postman", samplePostman)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Collection.Name != "Demo API" || res.Collection.Kind != "collection" {
		t.Errorf("collection = %+v", res.Collection)
	}
	if len(res.Collection.Variables) != 1 || res.Collection.Variables[0].Key != "base" {
		t.Errorf("vars = %+v", res.Collection.Variables)
	}
	if len(res.Children) != 3 { // folder + 2 requests
		t.Fatalf("children = %d, want 3", len(res.Children))
	}
	folder := res.Children[0]
	if folder.Kind != "folder" || folder.Name != "Users" {
		t.Errorf("folder = %+v", folder)
	}
	get := res.Children[1]
	if get.Kind != "request" || get.ParentId != folder.Id {
		t.Errorf("request parent = %+v", get)
	}
	if get.Request.Url != "{{base}}/users/1" || len(get.Request.Params) != 1 {
		t.Errorf("url/params = %q %+v", get.Request.Url, get.Request.Params)
	}
	if get.Request.Auth.Type != "bearer" || get.Request.Auth.Params["token"] != "{{tok}}" {
		t.Errorf("auth = %+v", get.Request.Auth)
	}
	if !strings.Contains(get.Request.TestScript, "pm.test") {
		t.Errorf("testScript = %q", get.Request.TestScript)
	}
	post := res.Children[2]
	if post.Request.Body.Kind != "raw" || post.Request.Body.Language != "json" {
		t.Errorf("body = %+v", post.Request.Body)
	}
}

func TestPostmanRoundtrip(t *testing.T) {
	res, err := Import("postman", samplePostman)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Export("postman", res.Collection, res.Children)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	// 导出物应可再导入且结构一致
	res2, err := Import("postman", out)
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if res2.Collection.Name != res.Collection.Name || len(res2.Children) != len(res.Children) {
		t.Errorf("roundtrip: %s %d vs %s %d",
			res.Collection.Name, len(res.Children), res2.Collection.Name, len(res2.Children))
	}
	// 导出 JSON 应含 schema 声明
	var raw map[string]any
	json.Unmarshal([]byte(out), &raw)
	info := raw["info"].(map[string]any)
	if !strings.Contains(info["schema"].(string), "v2.1.0") {
		t.Errorf("schema = %v", info["schema"])
	}
}

func TestAutoDetect(t *testing.T) {
	if _, err := Import("auto", samplePostman); err != nil {
		t.Errorf("auto postman: %v", err)
	}
	if _, err := Import("auto", `curl https://x.io`); err != nil {
		t.Errorf("auto curl: %v", err)
	}
	if _, err := Import("auto", `random text`); err == nil {
		t.Error("auto should fail on unknown format")
	}
}

func TestCurlImport(t *testing.T) {
	cmd := `curl -X POST 'https://api.demo.io/users?a=1' \
  -H 'Content-Type: application/json' \
  -H 'X-Key: k1' \
  -u admin:secret \
  -k \
  -d '{"name":"alice"}'`
	res, err := Import("curl", cmd)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	req := res.Children[0].Request
	if req.Method != "POST" || req.Url != "https://api.demo.io/users?a=1" {
		t.Errorf("req = %s %s", req.Method, req.Url)
	}
	if len(req.Headers) != 2 {
		t.Errorf("headers = %+v", req.Headers)
	}
	if req.Auth.Type != "basic" || req.Auth.Params["username"] != "admin" || req.Auth.Params["password"] != "secret" {
		t.Errorf("auth = %+v", req.Auth)
	}
	if req.Settings.VerifyTLS {
		t.Error("-k should disable TLS verify")
	}
	if req.Body.Kind != "raw" || req.Body.Language != "json" {
		t.Errorf("body = %+v", req.Body)
	}
}

func TestCurlFormAndDefaults(t *testing.T) {
	res, err := Import("curl", `curl https://x.io/upload -F "file=@/tmp/a.png" -F "name=pic"`)
	if err != nil {
		t.Fatal(err)
	}
	req := res.Children[0].Request
	if req.Method != "POST" {
		t.Errorf("method = %s, want POST (implied by body)", req.Method)
	}
	if req.Body.Kind != "formdata" || len(req.Body.Items) != 2 {
		t.Fatalf("body = %+v", req.Body)
	}
	if req.Body.Items[0].Type != "file" || req.Body.Items[0].Path != "/tmp/a.png" {
		t.Errorf("file item = %+v", req.Body.Items[0])
	}

	res2, _ := Import("curl", `curl https://x.io/`)
	if res2.Children[0].Request.Method != "GET" {
		t.Error("default method should be GET")
	}
}

func TestCurlUrlencodedBody(t *testing.T) {
	res, err := Import("curl", `curl https://x.io/login -d 'user=a' -d 'pass=b'`)
	if err != nil {
		t.Fatal(err)
	}
	req := res.Children[0].Request
	if req.Body.Kind != "urlencoded" || len(req.Body.Items) != 2 {
		t.Errorf("body = %+v", req.Body)
	}
}
