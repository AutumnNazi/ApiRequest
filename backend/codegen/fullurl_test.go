package codegen

import (
	"strings"
	"testing"

	"apirequest/backend/model"
)

// TestFullUrlQueryDedup 防 fullUrl 重复 query key 回归。
func TestFullUrlQueryDedup(t *testing.T) {
	req := model.HttpRequest{
		Method: "GET",
		Url:    "https://x.io/users?id=1",
		Params: []model.KV{
			{Key: "id", Value: "1", Enabled: true},
			{Key: "verbose", Value: "true", Enabled: true},
		},
		Auth:     model.Auth{Type: "none"},
		Settings: model.DefaultSettings(),
	}
	out, err := Generate("curl", req)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "id=1") != 1 {
		t.Errorf("id=1 应只出现一次，实际:\n%s", out)
	}
	if !strings.Contains(out, "verbose=true") {
		t.Errorf("verbose=true 应包含，实际:\n%s", out)
	}
}

// TestFullUrlPreservesVar 与 curl_export 行为对齐：保留 {{var}}
func TestFullUrlPreservesVar(t *testing.T) {
	req := model.HttpRequest{
		Method: "GET",
		Url:    "{{base}}/users",
		Params: []model.KV{
			{Key: "verbose", Value: "true", Enabled: true},
		},
		Auth:     model.Auth{Type: "none"},
		Settings: model.DefaultSettings(),
	}
	out, err := Generate("curl", req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "{{base}}/users") {
		t.Errorf("{{base}} 应原样保留，实际:\n%s", out)
	}
}

// TestJavaRustPhpCsharpFullUrlDedup 4 个新 generator 的 dedup 都生效
func TestJavaRustPhpCsharpFullUrlDedup(t *testing.T) {
	req := model.HttpRequest{
		Method: "GET",
		Url:    "https://x.io/users?id=1",
		Params: []model.KV{
			{Key: "id", Value: "1", Enabled: true},
			{Key: "verbose", Value: "true", Enabled: true},
		},
		Auth:     model.Auth{Type: "none"},
		Settings: model.DefaultSettings(),
	}
	for _, target := range []string{"java-httpclient", "rust-reqwest", "php-curl", "csharp-httpclient"} {
		out, err := Generate(target, req)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if strings.Count(out, "id=1") != 1 {
			t.Errorf("[%s] id=1 应只出现一次:\n%s", target, out)
		}
		if !strings.Contains(out, "verbose=true") {
			t.Errorf("[%s] verbose=true 应包含:\n%s", target, out)
		}
	}
}