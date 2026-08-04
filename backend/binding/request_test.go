package binding

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"apirequest/backend/httpengine"
	"apirequest/backend/model"
	"apirequest/backend/secrets"
	"apirequest/backend/storage"
)

// TestSendRequestPersistsHistory 验证"发送 → 响应 → 落历史"闭环
func TestSendRequestPersistsHistory(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"hello":"world"}`))
	}))
	defer srv.Close()

	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	w, _ := store.EnsureDefaultWorkspace()

	api := NewRequestApi(httpengine.New(), store)
	res, err := api.SendRequest("send-1", model.HttpRequest{
		Method: "GET", Url: srv.URL, Settings: model.DefaultSettings(),
	}, model.SendContext{WorkspaceId: w.Id})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if res.Status != 200 || res.HistoryId == "" {
		t.Fatalf("res = status %d, historyId %q", res.Status, res.HistoryId)
	}

	page, err := store.ListHistory(w.Id, model.HistoryQuery{})
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("history len = %d, want 1", len(page.Items))
	}
	detail, err := store.GetHistory(w.Id, res.HistoryId)
	if err != nil || page.Items[0].Id != res.HistoryId || detail.BodyInline != `{"hello":"world"}` {
		t.Errorf("history mismatch: summary=%+v detail=%+v err=%v", page.Items[0], detail, err)
	}
}

// TestCancelUnknownSendIdIsNoop 验证取消语义：未知 sendId 为 no-op
func TestCancelUnknownSendIdIsNoop(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	api := NewRequestApi(httpengine.New(), store)
	if err := api.CancelRequest("nonexistent"); err != nil {
		t.Errorf("cancel unknown = %v, want nil", err)
	}
}

func TestSendRequestRejectsCrossWorkspaceContextReferences(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	w1, _ := store.EnsureDefaultWorkspace()
	w2, _ := store.CreateWorkspace("second")
	col, _ := store.UpsertNode(model.Node{WorkspaceId: w1.Id, Kind: "collection", Name: "one"})
	node, _ := store.UpsertNode(model.Node{
		WorkspaceId: w1.Id, ParentId: col.Id, Kind: "request", Name: "request",
		Request: &model.HttpRequest{Method: "GET", Url: srv.URL, Settings: model.DefaultSettings()},
	})
	env, _ := store.UpsertEnvironment(model.Environment{WorkspaceId: w1.Id, Name: "dev"})
	api := NewRequestApi(httpengine.New(), store)
	req := model.HttpRequest{Method: "GET", Url: srv.URL, Settings: model.DefaultSettings()}

	if _, err := api.SendRequest("cross-node", req, model.SendContext{WorkspaceId: w2.Id, RequestId: node.Id}); err == nil {
		t.Fatal("cross-workspace requestId was accepted")
	}
	if _, err := api.SendRequest("cross-env", req, model.SendContext{WorkspaceId: w2.Id, EnvironmentId: env.Id}); err == nil {
		t.Fatal("cross-workspace environmentId was accepted")
	}
	if requests != 0 {
		t.Fatalf("network request ran before ownership validation: %d", requests)
	}
}

func TestSendRequestRedactsExpandedSecretsFromHistory(t *testing.T) {
	const secretValue = "history-variable-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	dataDir := t.TempDir()
	vault := secrets.NewWithKeyring(dataDir, nil)
	if err := vault.Unlock("binding-request-test"); err != nil {
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
	response, err := api.SendRequest("secret-history", model.HttpRequest{
		Method:   "POST",
		Url:      srv.URL + "/{{token}}",
		Params:   []model.KV{{Key: "token", Value: "{{token}}"}},
		Headers:  []model.KV{{Key: "X-Token", Value: "Bearer {{token}}", Enabled: true}},
		Body:     model.Body{Kind: "raw", Language: "json", Text: `{"token":"{{token}}"}`},
		Settings: model.DefaultSettings(),
	}, model.SendContext{WorkspaceId: workspace.Id, EnvironmentId: environment.Id})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := store.GetHistory(workspace.Id, response.HistoryId)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(detail.RequestSnap)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), secretValue) {
		t.Fatalf("history contains expanded secret: %s", raw)
	}
	if !strings.Contains(detail.RequestSnap.Url, "<redacted>") {
		t.Fatalf("history has no redaction marker: %s", raw)
	}
}
