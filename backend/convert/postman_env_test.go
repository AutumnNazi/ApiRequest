package convert

import (
	"encoding/json"
	"testing"
)

// Postman 环境文件导入：.postman_environment JSON → SuggestedEnvironments
//（集合为空占位——环境文件本身没有请求集合）
func TestPostmanEnvironmentImport(t *testing.T) {
	payload := `{
		"name": "Staging",
		"_postman_variable_scope": "environment",
		"values": [
			{"key": "baseUrl", "value": "https://staging.api.test"},
			{"key": "token", "value": "tk-123"},
			{"key": "disabledVar", "value": "x", "enabled": false}
		]
	}`
	res, err := Import("postman-env", payload)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(res.SuggestedEnvironments) != 1 {
		t.Fatalf("suggested environments = %d, want 1", len(res.SuggestedEnvironments))
	}
	env := res.SuggestedEnvironments[0]
	if env.Name != "Staging" {
		t.Fatalf("env name = %q, want 'Staging'", env.Name)
	}
	if len(env.Variables) != 2 {
		t.Fatalf("env variables = %d, want 2 (disabled excluded): %+v", len(env.Variables), env.Variables)
	}
	byKey := map[string]string{}
	for _, v := range env.Variables {
		byKey[v.Key] = v.Value
	}
	if byKey["baseUrl"] != "https://staging.api.test" || byKey["token"] != "tk-123" {
		t.Fatalf("variables wrong: %+v", byKey)
	}
	if _, ok := byKey["disabledVar"]; ok {
		t.Fatal("disabled variable must not import")
	}

	// 提交后由 ImportCommit 建 UpstreamEnvironment；本测试只断言结构可序列化
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw) {
		t.Fatal("import result not JSON-serializable")
	}
}

// auto 识别：环境文件与集合文件区分（scope 标记）
func TestPostmanEnvironmentDetect(t *testing.T) {
	payload := `{"name":"x","_postman_variable_scope":"environment","values":[{"key":"a","value":"1"}]}`
	res, err := Import("auto", payload)
	if err != nil {
		t.Fatalf("auto import: %v", err)
	}
	if len(res.SuggestedEnvironments) != 1 || res.SuggestedEnvironments[0].Name != "x" {
		t.Fatalf("auto should route env file to postman-env: %+v", res)
	}
	// 集合文件（collection scope）不会误判为环境
	colPayload := `{"info":{"name":"c","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},"item":[]}`
	if _, err := Import("auto", colPayload); err != nil {
		t.Fatalf("collection file must still import as collection: %v", err)
	}
}

// 非法：values 不是数组 / 缺 key → 结构化错误
func TestPostmanEnvironmentRejectsMalformed(t *testing.T) {
	if _, err := Import("postman-env", `{"name":"x","values":"nope"}`); err == nil {
		t.Fatal("malformed values must error")
	}
	if _, err := Import("postman-env", `{"values":[]}`); err == nil {
		t.Fatal("missing name must error")
	}
}

// 敏感键标记：token/password/secret/api_key 类 key 的变量 type=secret，
// 落库后走 Vault 保护（binding 的 lifecycle 链按 type=secret 收纳）
func TestPostmanEnvironmentMarksSecretKeys(t *testing.T) {
	payload := `{
		"name": "E",
		"_postman_variable_scope": "environment",
		"values": [
			{"key": "baseUrl", "value": "https://api.test"},
			{"key": "apiToken", "value": "tk"},
			{"key": "db_password", "value": "pw"},
			{"key": "clientSecret", "value": "s"},
			{"key": "X_API_KEY", "value": "k"}
		]
	}`
	res, err := Import("postman-env", payload)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	vars := res.SuggestedEnvironments[0].Variables
	types := map[string]string{}
	for _, v := range vars {
		types[v.Key] = v.Type
	}
	for _, secret := range []string{"apiToken", "db_password", "clientSecret", "X_API_KEY"} {
		if types[secret] != "secret" {
			t.Errorf("variable %q type = %q, want secret", secret, types[secret])
		}
	}
	if types["baseUrl"] != "default" {
		t.Errorf("baseUrl type = %q, want default", types["baseUrl"])
	}
}
