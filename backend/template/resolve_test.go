package template

import (
	"regexp"
	"strings"
	"testing"

	"apirequest/backend/model"
)

func scopeWith(kv map[string]string) *Scope {
	return NewScope().PushMap(kv)
}

func TestResolveBasic(t *testing.T) {
	s := scopeWith(map[string]string{"host": "api.example.com", "id": "42"})
	cases := map[string]string{
		"https://{{host}}/users/{{id}}": "https://api.example.com/users/42",
		"no vars here":                  "no vars here",
		"{{ host }}":                    "api.example.com", // 允许空格
		"{{undefined}}/x":               "{{undefined}}/x", // 未定义原样保留
		"{{unclosed":                    "{{unclosed",
	}
	for in, want := range cases {
		if got := Resolve(in, s); got != want {
			t.Errorf("Resolve(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveNested(t *testing.T) {
	s := scopeWith(map[string]string{
		"url":  "https://{{host}}/v1",
		"host": "api.example.com",
	})
	if got := Resolve("{{url}}/users", s); got != "https://api.example.com/v1/users" {
		t.Errorf("nested = %q", got)
	}
}

func TestResolveCircular(t *testing.T) {
	s := scopeWith(map[string]string{"a": "{{b}}", "b": "{{a}}"})
	// 不应死循环；到达最大深度后停止
	got := Resolve("{{a}}", s)
	if !strings.Contains(got, "{{") {
		t.Errorf("circular should stop at max depth, got %q", got)
	}
}

func TestScopePriority(t *testing.T) {
	// 低优先级先 push，高优先级后 push 覆盖
	s := NewScope().
		PushVariables([]model.Variable{{Key: "k", Value: "global", Enabled: true}}).
		PushVariables([]model.Variable{{Key: "k", Value: "env", Enabled: true}}).
		PushMap(map[string]string{"k": "local"})
	if v, _ := s.Get("k"); v != "local" {
		t.Errorf("priority = %q, want local", v)
	}
}

func TestDisabledVariableIgnored(t *testing.T) {
	s := NewScope().PushVariables([]model.Variable{{Key: "k", Value: "v", Enabled: false}})
	if _, ok := s.Get("k"); ok {
		t.Error("disabled variable should not be visible")
	}
}

func TestDynamicVars(t *testing.T) {
	s := NewScope()
	uuidRe := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	if got := Resolve("{{$guid}}", s); !uuidRe.MatchString(got) {
		t.Errorf("$guid = %q", got)
	}
	if got := Resolve("{{$timestamp}}", s); !regexp.MustCompile(`^\d{10}$`).MatchString(got) {
		t.Errorf("$timestamp = %q", got)
	}
	if got := Resolve("{{$randomEmail}}", s); !strings.Contains(got, "@example.com") {
		t.Errorf("$randomEmail = %q", got)
	}
	if got := Resolve("{{$unknownDynamic}}", s); got != "{{$unknownDynamic}}" {
		t.Errorf("unknown dynamic should keep original, got %q", got)
	}
}

func TestResolveRequest(t *testing.T) {
	s := scopeWith(map[string]string{"host": "h.io", "token": "T", "name": "n1"})
	req := model.HttpRequest{
		Method: "POST",
		Url:    "https://{{host}}/api",
		Params: []model.KV{{Key: "q", Value: "{{name}}", Enabled: true}},
		Headers: []model.KV{
			{Key: "Authorization", Value: "Bearer {{token}}", Enabled: true},
		},
		Body: model.Body{Kind: "raw", Language: "json", Text: `{"who":"{{name}}"}`},
		Auth: model.Auth{Type: "bearer", Params: map[string]string{"token": "{{token}}"}},
	}
	got := ResolveRequest(req, s)
	if got.Url != "https://h.io/api" {
		t.Errorf("url = %q", got.Url)
	}
	if got.Params[0].Value != "n1" || got.Headers[0].Value != "Bearer T" {
		t.Errorf("kv = %+v %+v", got.Params[0], got.Headers[0])
	}
	if got.Body.Text != `{"who":"n1"}` {
		t.Errorf("body = %q", got.Body.Text)
	}
	if got.Auth.Params["token"] != "T" {
		t.Errorf("auth = %+v", got.Auth.Params)
	}
	// 原请求不应被修改
	if req.Url != "https://{{host}}/api" {
		t.Error("input request mutated")
	}
}
