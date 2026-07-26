package codegen

import (
	"strings"
	"testing"

	"apirequest/backend/model"
)

func sampleReq() model.HttpRequest {
	return model.HttpRequest{
		Method: "POST",
		Url:    "https://api.demo.io/users",
		Params: []model.KV{
			{Key: "notify", Value: "true", Enabled: true},
			{Key: "skip", Value: "x", Enabled: false},
		},
		Headers: []model.KV{
			{Key: "X-Trace", Value: "t1", Enabled: true},
		},
		Body:     model.Body{Kind: "raw", Language: "json", Text: `{"name":"alice"}`},
		Auth:     model.Auth{Type: "bearer", Params: map[string]string{"token": "tok"}},
		Settings: model.DefaultSettings(),
	}
}

func TestTargets(t *testing.T) {
	targets := Targets()
	if len(targets) != 4 {
		t.Fatalf("targets = %d, want 4", len(targets))
	}
	if _, err := Generate("nonexistent", sampleReq()); err == nil {
		t.Error("unknown target should error")
	}
}

func TestCurlGen(t *testing.T) {
	out, err := Generate("curl", sampleReq())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"curl -X POST", "https://api.demo.io/users?notify=true",
		"-H 'X-Trace: t1'", "-H 'Authorization: Bearer tok'",
		"-H 'Content-Type: application/json'", `-d '{"name":"alice"}'`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("curl missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "skip=x") {
		t.Error("disabled param should be excluded")
	}
}

func TestFetchGen(t *testing.T) {
	out, err := Generate("javascript-fetch", sampleReq())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`await fetch("https://api.demo.io/users?notify=true"`,
		`method: "POST"`, `"Authorization": "Bearer tok"`,
		`body: "{\"name\":\"alice\"}"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("fetch missing %q in:\n%s", want, out)
		}
	}
}

func TestPythonGen(t *testing.T) {
	req := sampleReq()
	req.Auth = model.Auth{Type: "basic", Params: map[string]string{"username": "u", "password": "p"}}
	out, err := Generate("python-requests", req)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"import requests", `url = "https://api.demo.io/users?notify=true"`,
		"requests.post(", `auth=("u", "p")`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("python missing %q in:\n%s", want, out)
		}
	}
}

func TestGoGen(t *testing.T) {
	out, err := Generate("go-nethttp", sampleReq())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`http.NewRequest("POST"`, "strings.NewReader",
		`req.Header.Set("Authorization", "Bearer tok")`,
		"http.DefaultClient.Do(req)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("go missing %q in:\n%s", want, out)
		}
	}
}

func TestUrlencodedBodyAcrossTargets(t *testing.T) {
	req := model.HttpRequest{
		Method: "POST", Url: "https://x.io/login",
		Body: model.Body{Kind: "urlencoded", Items: []model.FormItem{
			{Key: "user", Value: "a b", Enabled: true, Type: "text"},
		}},
		Auth: model.Auth{Type: "none"}, Settings: model.DefaultSettings(),
	}
	for _, target := range []string{"curl", "javascript-fetch", "python-requests", "go-nethttp"} {
		out, err := Generate(target, req)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if !strings.Contains(out, "user=a+b") {
			t.Errorf("%s missing urlencoded body:\n%s", target, out)
		}
	}
}
