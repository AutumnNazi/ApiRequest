package storage

import (
	"os"
	"path/filepath"
	"testing"

	"apirequest/backend/model"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestMigrateAndReopen(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	s.Close()
	// 二次打开：迁移应幂等
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	s2.Close()
}

func TestWorkspaceAndNodeCrud(t *testing.T) {
	s := openTestStore(t)

	w, err := s.EnsureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensure workspace: %v", err)
	}
	// 幂等：再调用返回同一个
	w2, _ := s.EnsureDefaultWorkspace()
	if w2.Id != w.Id {
		t.Errorf("workspace not idempotent: %s vs %s", w.Id, w2.Id)
	}

	col, err := s.UpsertNode(model.Node{WorkspaceId: w.Id, Kind: "collection", Name: "API 集合"})
	if err != nil {
		t.Fatalf("insert collection: %v", err)
	}
	req, err := s.UpsertNode(model.Node{
		WorkspaceId: w.Id, ParentId: col.Id, Kind: "request", Name: "获取用户",
		Request: &model.HttpRequest{Method: "GET", Url: "https://example.com/users", Settings: model.DefaultSettings()},
	})
	if err != nil {
		t.Fatalf("insert request: %v", err)
	}

	nodes, err := s.ListNodes(w.Id)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("len(nodes) = %d, want 2", len(nodes))
	}

	// 更新：改名 + 改 URL
	req.Name = "获取用户列表"
	req.Request.Url = "https://example.com/users?page=1"
	if _, err := s.UpsertNode(req); err != nil {
		t.Fatalf("update: %v", err)
	}
	nodes, _ = s.ListNodes(w.Id)
	var found *model.Node
	for i := range nodes {
		if nodes[i].Id == req.Id {
			found = &nodes[i]
		}
	}
	if found == nil || found.Name != "获取用户列表" || found.Request.Url != "https://example.com/users?page=1" {
		t.Errorf("update not persisted: %+v", found)
	}

	// 软删除集合应级联到子请求
	if err := s.DeleteNode(col.Id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	nodes, _ = s.ListNodes(w.Id)
	if len(nodes) != 0 {
		t.Errorf("after cascade delete len = %d, want 0", len(nodes))
	}
}

func TestMoveNodeRejectsInvalidTrees(t *testing.T) {
	s := openTestStore(t)
	w, _ := s.EnsureDefaultWorkspace()
	col, _ := s.UpsertNode(model.Node{WorkspaceId: w.Id, Kind: "collection", Name: "collection"})
	folder, _ := s.UpsertNode(model.Node{WorkspaceId: w.Id, ParentId: col.Id, Kind: "folder", Name: "folder"})
	child, _ := s.UpsertNode(model.Node{WorkspaceId: w.Id, ParentId: folder.Id, Kind: "folder", Name: "child"})
	req, _ := s.UpsertNode(model.Node{WorkspaceId: w.Id, ParentId: folder.Id, Kind: "request", Name: "request"})

	for _, tc := range []struct {
		name, id, parent string
	}{
		{"missing node", "missing", col.Id},
		{"missing parent", req.Id, "missing"},
		{"request parent", req.Id, req.Id},
		{"folder into descendant", folder.Id, child.Id},
		{"collection below root", col.Id, folder.Id},
		{"request at root", req.Id, ""},
		{"folder at root", folder.Id, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.MoveNode(tc.id, tc.parent, 1); err == nil {
				t.Fatal("MoveNode succeeded, want validation error")
			}
		})
	}
	if err := s.MoveNode(req.Id, col.Id, 2); err != nil {
		t.Fatalf("valid move: %v", err)
	}
}

func TestHistory(t *testing.T) {
	s := openTestStore(t)
	w, _ := s.EnsureDefaultWorkspace()

	id, err := s.InsertHistory(model.HistoryItem{
		WorkspaceId: w.Id,
		RequestSnap: model.HttpRequest{Method: "GET", Url: "https://example.com/a"},
		Status:      200, DurationMs: 12, SizeBytes: 34,
		Timing:      model.Timing{TotalMs: 12.5},
		RespHeaders: []model.KV{{Key: "Content-Type", Value: "application/json", Enabled: true}},
		BodyInline:  `{"ok":true}`,
		TestResults: []model.TestResult{{Name: "status is 200", Pass: true}},
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if id == "" {
		t.Fatal("empty history id")
	}

	items, err := s.ListHistory(w.Id, model.HistoryQuery{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len = %d, want 1", len(items))
	}
	it := items[0]
	if it.RequestSnap.Url != "https://example.com/a" || it.Status != 200 ||
		it.BodyInline != `{"ok":true}` || len(it.TestResults) != 1 || it.Timing.TotalMs != 12.5 {
		t.Errorf("roundtrip mismatch: %+v", it)
	}

	// 搜索过滤
	items, _ = s.ListHistory(w.Id, model.HistoryQuery{Search: "example.com"})
	if len(items) != 1 {
		t.Errorf("search hit = %d, want 1", len(items))
	}
	items, _ = s.ListHistory(w.Id, model.HistoryQuery{Search: "nomatch-xyz"})
	if len(items) != 0 {
		t.Errorf("search miss = %d, want 0", len(items))
	}

	if err := s.ClearHistory(w.Id); err != nil {
		t.Fatalf("clear: %v", err)
	}
	items, _ = s.ListHistory(w.Id, model.HistoryQuery{})
	if len(items) != 0 {
		t.Errorf("after clear len = %d, want 0", len(items))
	}
}

func TestClearHistoryRemovesBlob(t *testing.T) {
	s := openTestStore(t)
	w, _ := s.EnsureDefaultWorkspace()
	if err := os.MkdirAll(s.BlobsDir(), 0o755); err != nil {
		t.Fatalf("mkdir blobs: %v", err)
	}
	const ref = "history-test.bin"
	path := filepath.Join(s.BlobsDir(), ref)
	if err := os.WriteFile(path, []byte("blob"), 0o600); err != nil {
		t.Fatalf("write blob: %v", err)
	}
	if _, err := s.InsertHistory(model.HistoryItem{
		WorkspaceId: w.Id,
		RequestSnap: model.HttpRequest{Method: "GET", Url: "https://example.com"},
		BodyRef:     ref,
	}); err != nil {
		t.Fatalf("insert history: %v", err)
	}

	if err := s.ClearHistory(w.Id); err != nil {
		t.Fatalf("clear history: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("blob still exists, stat error = %v", err)
	}
}
