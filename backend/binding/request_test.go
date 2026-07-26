package binding

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"apirequest/backend/httpengine"
	"apirequest/backend/model"
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

	items, err := store.ListHistory(w.Id, model.HistoryQuery{})
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("history len = %d, want 1", len(items))
	}
	if items[0].Id != res.HistoryId || items[0].BodyInline != `{"hello":"world"}` {
		t.Errorf("history mismatch: %+v", items[0])
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
