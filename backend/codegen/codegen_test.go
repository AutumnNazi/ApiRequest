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
	if len(targets) != 8 {
		t.Fatalf("targets = %d, want 8", len(targets))
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

func TestCurlGenPreservesMultipartFilesAndBinaryBodies(t *testing.T) {
	form := sampleReq()
	form.Body = model.Body{Kind: "formdata", Items: []model.FormItem{
		{Key: "note", Type: "text", Value: "hello", Enabled: true},
		{Key: "asset", Type: "file", Path: "/tmp/a'b.bin", Enabled: true},
	}}
	out, err := Generate("curl", form)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--form-string 'note=hello'", `-F 'asset=@/tmp/a'\''b.bin'`} {
		if !strings.Contains(out, want) {
			t.Errorf("multipart curl missing %q in:\n%s", want, out)
		}
	}

	binary := sampleReq()
	binary.Body = model.Body{Kind: "binary", Path: "/tmp/data.bin"}
	out, err = Generate("curl", binary)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "--data-binary '@/tmp/data.bin'") ||
		!strings.Contains(out, "Content-Type: application/octet-stream") {
		t.Errorf("binary curl is incomplete:\n%s", out)
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
		`requests.request("POST", url`, `auth=("u", "p")`,
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

func TestJavaGen(t *testing.T) {
	out, err := Generate("java-httpclient", sampleReq())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"HttpClient.newHttpClient()", "URI.create(\"https://api.demo.io/users?notify=true\")",
		`.method("POST"`, `.header("Authorization", "Bearer tok")`,
		"HttpResponse.BodyHandlers.ofString()",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("java missing %q in:\n%s", want, out)
		}
	}
}

func TestRustGen(t *testing.T) {
	out, err := Generate("rust-reqwest", sampleReq())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"use std::error::Error", "Client::new()",
		`client.request("POST".parse()?, "https://api.demo.io/users?notify=true")`,
		`.header("Authorization", "Bearer tok")`,
		".send().await",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rust missing %q in:\n%s", want, out)
		}
	}
}

func TestPhpGen(t *testing.T) {
	out, err := Generate("php-curl", sampleReq())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"$ch = curl_init('https://api.demo.io/users?notify=true')",
		"CURLOPT_CUSTOMREQUEST", "CURLOPT_RETURNTRANSFER",
		"'Authorization: Bearer tok'", "'X-Trace: t1'",
		"curl_exec($ch)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("php missing %q in:\n%s", want, out)
		}
	}
}

func TestCsharpGen(t *testing.T) {
	out, err := Generate("csharp-httpclient", sampleReq())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"new HttpClient()", "new HttpMethod(\"POST\")",
		"req.Headers.TryAddWithoutValidation(\"Authorization\", \"Bearer tok\");",
		"req.Headers.TryAddWithoutValidation(\"X-Trace\", \"t1\");",
		"client.SendAsync(req)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("csharp missing %q in:\n%s", want, out)
		}
	}
}

func TestPythonCustomMethod(t *testing.T) {
	req := sampleReq()
	req.Method = "MKCOL"
	out, err := Generate("python-requests", req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `requests.request("MKCOL", url`) {
		t.Fatalf("custom method should use requests.request:\n%s", out)
	}
}

func TestApiKeyQueryAcrossCodegen(t *testing.T) {
	req := model.HttpRequest{
		Method: "GET", Url: "https://x.io/items",
		Auth: model.Auth{Type: "apikey", Params: map[string]string{
			"in": "query", "key": "api_key", "value": "a b",
		}},
		Settings: model.DefaultSettings(),
	}
	for _, target := range []string{"curl", "javascript-fetch", "python-requests", "go-nethttp"} {
		out, err := Generate(target, req)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if !strings.Contains(out, "api_key=a+b") {
			t.Errorf("%s missing query API key:\n%s", target, out)
		}
		if strings.Contains(out, "api_key: a b") {
			t.Errorf("%s incorrectly placed query API key in a header:\n%s", target, out)
		}
	}
}
