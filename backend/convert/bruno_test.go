package convert

import (
	"strings"
	"testing"
)

const brunoSample = `meta {
  name: Get user
  type: http
  seq: 1
}

get {
  url: https://api.example.com/users/:id
  params: {
    limit: 10
    ~page: 0
  }
  headers {
    content-type: application/json
    ~x-disabled: 1
    x-token: {{token}}
  }
  auth: bearer {
    token: tk-123
  }
}

body:json {
  {"name": "张三", "tags": ["a", "b"]}
}

script:pre {
  console.log('before');
}

script:post {
  pm.test('ok', function () { pm.expect(1).to.equal(1); });
}

vars:pre {
  token: abc
}

docs {
  Some docs here
}
`

func TestBrunoImport(t *testing.T) {
	result, err := Import("bruno", brunoSample)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if result.Collection.Name != "Get user" {
		t.Fatalf("collection name = %q", result.Collection.Name)
	}
	if len(result.Children) != 1 {
		t.Fatalf("children = %d", len(result.Children))
	}
	req := result.Children[0].Request
	if req == nil {
		t.Fatal("child has no request")
	}
	if req.Method != "GET" || req.Url != "https://api.example.com/users/:id" {
		t.Fatalf("request line = %s %s", req.Method, req.Url)
	}
	// params：禁用行保留且标记 enabled=false
	if len(req.Params) != 2 || req.Params[0].Key != "limit" || req.Params[0].Value != "10" || !req.Params[0].Enabled {
		t.Fatalf("params = %+v", req.Params)
	}
	if req.Params[1].Key != "page" || req.Params[1].Enabled {
		t.Fatalf("disabled param = %+v", req.Params[1])
	}
	// headers：~ 前缀禁用，模板保留
	if len(req.Headers) != 3 || req.Headers[2].Value != "{{token}}" {
		t.Fatalf("headers = %+v", req.Headers)
	}
	for _, h := range req.Headers {
		if h.Key == "x-disabled" && h.Enabled {
			t.Fatalf("disabled header kept enabled: %+v", h)
		}
	}
	// auth
	if req.Auth.Type != "bearer" || req.Auth.Params["token"] != "tk-123" {
		t.Fatalf("auth = %+v", req.Auth)
	}
	// body:json 原样（含嵌套大括号与 UTF-8）
	if !strings.Contains(req.Body.Text, `"name": "张三"`) || !strings.Contains(req.Body.Text, `"tags": ["a", "b"]`) {
		t.Fatalf("body = %q", req.Body.Text)
	}
	if req.Body.Kind != "raw" || req.Body.Language != "json" {
		t.Fatalf("body kind = %s/%s", req.Body.Kind, req.Body.Language)
	}
	// scripts
	if req.PreScript != "console.log('before');" {
		t.Fatalf("preScript = %q", req.PreScript)
	}
	if !strings.Contains(req.TestScript, "pm.expect(1)") {
		t.Fatalf("testScript = %q", req.TestScript)
	}
	// vars:pre 无落点 → Warnings
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "variable") {
			found = true
		}
	}
	if !found {
		t.Fatalf("warnings = %v, want vars skip notice", result.Warnings)
	}
}

func TestBrunoDetect(t *testing.T) {
	bi := brunoImporter{}
	if !bi.Detect(brunoSample) {
		t.Fatal("detect failed on valid .bru payload")
	}
	if bi.Detect("GET https://x.test\r\nContent-Type: application/json") {
		t.Fatal("detect must not match restclient payload")
	}
	if bi.Detect("") {
		t.Fatal("detect must reject empty payload")
	}
	// auto 识别链命中 bruno
	result, err := Import("auto", brunoSample)
	if err != nil {
		t.Fatalf("auto import: %v", err)
	}
	if result.Collection.Name != "Get user" {
		t.Fatalf("auto import collection = %q", result.Collection.Name)
	}
}

func TestBrunoImportFormUrlEncodedAndText(t *testing.T) {
	payload := `meta {
  name: Form post
  type: http
}

post {
  url: https://api.example.com/form
}

body:formUrlEncoded {
  username: alice
  password: 's3cret'
}

body:text {
  plain text body
}
`
	result, err := Import("bruno", payload)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	req := result.Children[0].Request
	if req.Method != "POST" {
		t.Fatalf("method = %s", req.Method)
	}
	// 两个 body 块同存时后者覆盖（.bru 规范单文件单 body，容忍双写取最后）
	if req.Body.Kind != "raw" {
		t.Fatalf("body kind = %s", req.Body.Kind)
	}
}
