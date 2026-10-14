package binding

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"apirequest/backend/httpengine"
	"apirequest/backend/model"
	"apirequest/backend/secrets"
	"apirequest/backend/storage"
)

// WireDump 是实际发出请求的凭据载体（Authorization/Cookie 注入后的头）：
// 透出前端前必须把命中的机密值替换为掩码，否则调试面板变成凭据泄露面。
func TestSendRequestRedactsWireDumpSecrets(t *testing.T) {
	const secretValue = "wire-dump-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	dataDir := t.TempDir()
	vault := secrets.NewWithKeyring(dataDir, nil)
	if err := vault.Unlock("binding-wire-test"); err != nil {
		t.Fatal(err)
	}
	store, err := storage.OpenWithVault(dataDir, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	workspace, _ := store.EnsureDefaultWorkspace()
	environment, err := store.UpsertEnvironment(model.Environment{
		WorkspaceId: workspace.Id,
		Name:        "secured",
		Variables: []model.Variable{
			{Key: "token", Value: secretValue, Type: "secret", Enabled: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	api := NewRequestApi(httpengine.New(), store)
	response, err := api.SendRequest("wire-dump", model.HttpRequest{
		Method:   "GET",
		Url:      srv.URL + "/secure",
		Headers:  []model.KV{{Key: "Authorization", Value: "Bearer {{token}}", Enabled: true}},
		Settings: model.DefaultSettings(),
	}, model.SendContext{WorkspaceId: workspace.Id, EnvironmentId: environment.Id})
	if err != nil {
		t.Fatal(err)
	}

	if response.Wire == nil {
		t.Fatal("wire dump missing")
	}
	if response.Wire.Method != http.MethodGet || response.Wire.Url != srv.URL+"/secure" {
		t.Fatalf("wire request line = %s %s", response.Wire.Method, response.Wire.Url)
	}
	authIdx := slices.IndexFunc(response.Wire.Headers, func(h model.KV) bool {
		return h.Key == "Authorization"
	})
	if authIdx < 0 {
		t.Fatalf("wire headers missing Authorization: %+v", response.Wire.Headers)
	}
	if response.Wire.Headers[authIdx].Value != "<redacted>" {
		t.Fatalf("wire Authorization = %q, want redacted", response.Wire.Headers[authIdx].Value)
	}
	// 双保险：整段 wire 序列化不含机密原文
	for _, h := range response.Wire.Headers {
		if h.Value == "Bearer "+secretValue || h.Value == secretValue {
			t.Fatalf("wire header %q carries the raw secret", h.Key)
		}
	}
}
