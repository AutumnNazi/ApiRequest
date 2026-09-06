package codegen

import (
	"strings"
	"testing"

	"apirequest/backend/model"
)

func codegenFixtureReq() model.HttpRequest {
	return model.HttpRequest{
		Method: "POST",
		Url:    "https://api.test/users?page=2",
		Params: []model.KV{{Key: "page", Value: "2", Enabled: true}},
		Headers: []model.KV{
			{Key: "X-Token", Value: "abc", Enabled: true},
			{Key: "X-Off", Value: "off", Enabled: false},
		},
		Body:     model.Body{Kind: "raw", Language: "json", Text: `{"name":"n"}`},
		Auth:     model.Auth{Type: "bearer", Params: map[string]string{"token": "tk"}},
		Settings: model.DefaultSettings(),
	}
}

// HTTPie：方法+URL 同段、头作 : 语法、--auth=bearer、JSON body 原样
func TestHttpieGen(t *testing.T) {
	out := mustGenerate(t, "shell-httpie", codegenFixtureReq())
	if !strings.Contains(out, "http POST 'https://api.test/users?page=2'") {
		t.Fatalf("httpie line wrong: %s", out)
	}
	if !strings.Contains(out, `X-Token:abc`) {
		t.Fatalf("missing header flag: %s", out)
	}
	if strings.Contains(out, "X-Off") {
		t.Fatalf("disabled header must not appear: %s", out)
	}
	if !strings.Contains(out, `--auth-type=bearer --auth='tk'`) {
		t.Fatalf("missing bearer auth: %s", out)
	}
	if !strings.Contains(out, `<<< '{"name":"n"}'`) {
		t.Fatalf("missing raw JSON stdin: %s", out)
	}
}

// HTTPie：JSON body 时自动 Content-Type 可省略；form 走 --form 字段
func TestHttpieGenFormAndNoAuth(t *testing.T) {
	req := model.HttpRequest{
		Method: "PUT", Url: "https://api.test/f",
		Body:   model.Body{Kind: "urlencoded", Items: []model.FormItem{{Key: "a", Value: "1", Type: "text", Enabled: true}}},
		Settings: model.DefaultSettings(),
	}
	out := mustGenerate(t, "shell-httpie", req)
	if !strings.Contains(out, "http PUT") {
		t.Fatalf("PUT line wrong: %s", out)
	}
	if !strings.Contains(out, "--form") || !strings.Contains(out, "a=1") {
		t.Fatalf("form body missing: %s", out)
	}
	if strings.Contains(out, "--auth") {
		t.Fatalf("no auth configured: %s", out)
	}
}

// axios：axios({ url, method, headers, data }) + auth.token
func TestAxiosGen(t *testing.T) {
	req := codegenFixtureReq()
	out := mustGenerate(t, "javascript-axios", req)
	if !strings.Contains(out, `const { data } = await axios(`) {
		t.Fatalf("axios call missing: %s", out)
	}
	if !strings.Contains(out, `url: "https://api.test/users?page=2"`) {
		t.Fatalf("url missing: %s", out)
	}
	if !strings.Contains(out, `method: "POST"`) {
		t.Fatalf("method missing: %s", out)
	}
	if !strings.Contains(out, `"X-Token": "abc"`) {
		t.Fatalf("header missing: %s", out)
	}
	if !strings.Contains(out, `data: "{\"name\":\"n\"}"`) {
		t.Fatalf("data missing: %s", out)
	}
	if strings.Contains(out, "X-Off") {
		t.Fatalf("disabled header leaked: %s", out)
	}
}

// axios：basic auth → auth: { username, password }
func TestAxiosGenBasicAuth(t *testing.T) {
	req := model.HttpRequest{
		Method: "GET", Url: "https://api.test/ping",
		Auth:     model.Auth{Type: "basic", Params: map[string]string{"username": "u", "password": "p"}},
		Settings: model.DefaultSettings(),
	}
	out := mustGenerate(t, "javascript-axios", req)
	if !strings.Contains(out, "auth: {") || !strings.Contains(out, "username: \"u\"") || !strings.Contains(out, "password: \"p\"") {
		t.Fatalf("basic auth missing: %s", out)
	}
}

func mustGenerate(t *testing.T, id string, req model.HttpRequest) string {
	t.Helper()
	out, err := Generate(id, req)
	if err != nil {
		t.Fatalf("generate %s: %v", id, err)
	}
	return out
}
