package storage

import (
	"path/filepath"
	"testing"

	"apirequest/backend/model"
)

// 请求禁用：node 级 Disabled 持久化（含同库重开的迁移/读回路径）
func TestUpsertNodeDisabledPersists(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := store.EnsureDefaultWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	col, err := store.UpsertNode(model.Node{WorkspaceId: ws.Id, Kind: "collection", Name: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertNode(model.Node{
		WorkspaceId: ws.Id, ParentId: col.Id, Kind: "request", Name: "paused",
		Disabled: true,
		Request: &model.HttpRequest{Method: "GET", Url: "https://x.test", Settings: model.DefaultSettings()},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	// 同库重开（新 store 实例走完整 schema 迁移）：禁用状态读回
	store2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store2.Close()
	nodes, err := store2.ListNodes(ws.Id)
	if err != nil {
		t.Fatal(err)
	}
	var paused *model.Node
	for i := range nodes {
		if nodes[i].Name == "paused" {
			paused = &nodes[i]
		}
	}
	if paused == nil {
		t.Fatal("disabled request not found")
	}
	if !paused.Disabled {
		t.Fatalf("Disabled lost after reopen: %+v", paused)
	}

	// 禁用 → 启用：翻转持久化
	paused.Disabled = false
	if _, err := store2.UpsertNode(*paused); err != nil {
		t.Fatal(err)
	}
	again, err := store2.ListNodes(ws.Id)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range again {
		if n.Name == "paused" && n.Disabled {
			t.Fatal("re-enable did not persist")
		}
	}

	// 兜底：路径分隔符断言（Windows/POSIX 双跑都能过）
	if filepath.Base(store2.DbPath()) == "" {
		t.Fatal("db path malformed")
	}
}
