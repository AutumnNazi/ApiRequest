package convert

import (
	"testing"
)

func TestOpenApiImportSuggestsEnvFromServers(t *testing.T) {
	payload := `{
		"openapi": "3.0.3",
		"info": {"title": "订单服务", "version": "1.0"},
		"servers": [
			{"url": "https://api.example.com/v1"},
			{"url": "https://staging.example.com/v1"}
		],
		"paths": {
			"/orders": {"get": {"responses": {"200": {"description": "ok"}}}}
		}
	}`
	res, err := Import("openapi", payload)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(res.SuggestedEnvironments) == 0 {
		t.Fatal("SuggestedEnvironments missing")
	}
	if len(res.SuggestedEnvironments) != 2 {
		t.Fatalf("environments = %d, want 2 (每服务器一个)", len(res.SuggestedEnvironments))
	}
	first := res.SuggestedEnvironments[0].Variables[0]
	if first.Key != "baseUrl" || first.Value != "https://api.example.com/v1" || !first.Enabled {
		t.Fatalf("first env variable = %+v", first)
	}
	if res.SuggestedEnvironments[0].Name == "" {
		t.Fatalf("env name empty")
	}
	second := res.SuggestedEnvironments[1].Variables[0]
	if second.Value != "https://staging.example.com/v1" {
		t.Fatalf("second env baseUrl = %q", second.Value)
	}
}

func TestSwagger2ImportSuggestsEnvFromHost(t *testing.T) {
	payload := `{
		"swagger": "2.0",
		"info": {"title": "老服务", "version": "1.0"},
		"host": "api.example.com",
		"basePath": "/v2",
		"schemes": ["https"],
		"paths": {
			"/ping": {"get": {"responses": {"200": {"description": "ok"}}}}
		}
	}`
	res, err := Import("openapi", payload)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(res.SuggestedEnvironments) == 0 {
		t.Fatal("SuggestedEnvironments missing")
	}
	want := "https://api.example.com/v2"
	found := false
	for _, v := range res.SuggestedEnvironments[0].Variables {
		if v.Key == "baseUrl" && v.Value == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("baseUrl=%s missing, env=%+v", want, res.SuggestedEnvironments)
	}
}

func TestImportWithoutServersHasNoSuggestedEnv(t *testing.T) {
	payload := `{"openapi": "3.0.3", "info": {"title": "x", "version": "1"}, "paths": {}}`
	res, err := Import("openapi", payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.SuggestedEnvironments) > 0 {
		t.Fatalf("SuggestedEnvironments = %+v, want empty", res.SuggestedEnvironments)
	}
}
