package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

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

func insertLegacyHistoryBlob(t *testing.T, store *Store, workspaceId, ref string) {
	t.Helper()
	if _, err := store.db.Exec(`
		INSERT INTO history (id, workspace_id, request_snap, method, url, response_meta, body_ref, created_at)
		VALUES (?, ?, '{}', 'GET', 'https://example.test', '{}', ?, ?)`,
		newId(), workspaceId, ref, nowMs()); err != nil {
		t.Fatalf("insert legacy history blob: %v", err)
	}
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

func TestOpenCreatesBlobDirectory(t *testing.T) {
	store := openTestStore(t)
	info, err := os.Stat(store.BlobsDir())
	if err != nil {
		t.Fatalf("stat blobs directory: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("blob path is not a directory: %s", store.BlobsDir())
	}
}

func TestHistoryProjectionMigrationToleratesInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "apirequest.db"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if _, err := db.Exec(migrations[i]); err != nil {
			db.Close()
			t.Fatalf("apply migration %d: %v", i+1, err)
		}
	}
	if _, err := db.Exec("PRAGMA user_version = 4"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO workspace (id, name, type, created_at, updated_at) VALUES ('w1', 'test', 'local', 1, 1)"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO history (id, workspace_id, request_snap, created_at) VALUES ('h1', 'w1', 'not-json', 1)"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(dir)
	if err != nil {
		t.Fatalf("open with malformed legacy history: %v", err)
	}
	defer store.Close()
	var method, url string
	if err := store.db.QueryRow("SELECT method, url FROM history WHERE id = 'h1'").Scan(&method, &url); err != nil {
		t.Fatal(err)
	}
	if method != "" || url != "" {
		t.Fatalf("invalid snapshot projection = method %q, url %q", method, url)
	}
}

func TestHistoryOwnershipMigrationRemovesLegacyResponseBlobs(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "apirequest.db"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 7; i++ {
		if _, err := db.Exec(migrations[i]); err != nil {
			db.Close()
			t.Fatalf("apply migration %d: %v", i+1, err)
		}
	}
	if _, err := db.Exec("PRAGMA user_version = 7"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO workspace (id, name, type, created_at, updated_at) VALUES ('w1', 'test', 'local', 1, 1)"); err != nil {
		db.Close()
		t.Fatal(err)
	}
	for _, row := range []struct {
		id, workspaceId, ref string
	}{
		{id: "valid-history", workspaceId: "w1", ref: "legacy-valid.bin"},
		{id: "orphan-history", workspaceId: "missing", ref: "legacy-orphan.bin"},
	} {
		if _, err := db.Exec(`
			INSERT INTO history (id, workspace_id, request_snap, method, url, response_meta, body_ref, created_at)
			VALUES (?, ?, '{}', 'GET', 'https://example.test', '{}', ?, 1)`,
			row.id, row.workspaceId, row.ref); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "blobs"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"legacy-valid.bin", "legacy-orphan.bin"} {
		if err := os.WriteFile(filepath.Join(dir, "blobs", ref), []byte("legacy secret"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var bodyRef sql.NullString
	if err := store.db.QueryRow("SELECT body_ref FROM history WHERE id = 'valid-history'").Scan(&bodyRef); err != nil {
		t.Fatal(err)
	}
	if bodyRef.Valid {
		t.Fatalf("legacy history body ref survived migration: %q", bodyRef.String)
	}
	var orphanCount int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM history WHERE id = 'orphan-history'").Scan(&orphanCount); err != nil {
		t.Fatal(err)
	}
	if orphanCount != 0 {
		t.Fatal("orphan history survived ownership migration")
	}
	for _, ref := range []string{"legacy-valid.bin", "legacy-orphan.bin"} {
		if _, err := os.Stat(filepath.Join(store.BlobsDir(), ref)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("legacy response blob %q survived migration: %v", ref, err)
		}
	}
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

func TestWorkspaceOwnershipIsImmutable(t *testing.T) {
	s := openTestStore(t)
	w1, _ := s.EnsureDefaultWorkspace()
	w2, _ := s.CreateWorkspace("second")
	col1, _ := s.UpsertNode(model.Node{WorkspaceId: w1.Id, Kind: "collection", Name: "one"})
	col2, _ := s.UpsertNode(model.Node{WorkspaceId: w2.Id, Kind: "collection", Name: "two"})

	changedWorkspace := col1
	changedWorkspace.WorkspaceId = w2.Id
	if _, err := s.UpsertNode(changedWorkspace); err == nil {
		t.Fatal("cross-workspace node update succeeded")
	}
	if _, err := s.UpsertNode(model.Node{
		WorkspaceId: w1.Id, ParentId: col2.Id, Kind: "request", Name: "cross-parent",
	}); err == nil {
		t.Fatal("cross-workspace parent succeeded")
	}
	if _, err := s.NodeAncestorsInWorkspace(col1.Id, w2.Id); err == nil {
		t.Fatal("cross-workspace ancestor lookup succeeded")
	}

	env, _ := s.UpsertEnvironment(model.Environment{WorkspaceId: w1.Id, Name: "dev"})
	env.WorkspaceId = w2.Id
	if _, err := s.UpsertEnvironment(env); err == nil {
		t.Fatal("cross-workspace environment update succeeded")
	}
}

func TestApplySyncNodeRejectsCrossWorkspaceParent(t *testing.T) {
	s := openTestStore(t)
	w1, _ := s.EnsureDefaultWorkspace()
	w2, _ := s.CreateWorkspace("second")
	foreignParent, _ := s.UpsertNode(model.Node{WorkspaceId: w2.Id, Kind: "collection", Name: "foreign"})

	err := s.ApplySyncNode(SyncNodeRow{Node: model.Node{
		Id: "synced-child", WorkspaceId: w1.Id, ParentId: foreignParent.Id,
		Kind: "request", Name: "malicious",
	}})
	if err == nil {
		t.Fatal("sync node with a cross-workspace parent was accepted")
	}
}

func TestApplySyncEnvironmentDoesNotImportActiveState(t *testing.T) {
	s := openTestStore(t)
	workspace, _ := s.EnsureDefaultWorkspace()
	environment := model.Environment{
		Id: "synced-environment", WorkspaceId: workspace.Id, Name: "remote", IsActive: true,
		CreatedAt: 1, UpdatedAt: 1,
	}
	if err := s.ApplySyncEnvironment(environment); err != nil {
		t.Fatal(err)
	}
	stored, err := s.GetEnvironment(environment.Id)
	if err != nil {
		t.Fatal(err)
	}
	if stored.IsActive {
		t.Fatal("remote environment activation leaked into local UI state")
	}
}

func TestGlobalVariablesUseIndependentSyncRevision(t *testing.T) {
	s := openTestStore(t)
	workspace, _ := s.EnsureDefaultWorkspace()
	if err := s.SetGlobalVariables(workspace.Id, []model.Variable{{Key: "region", Value: "local", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	localRevision, err := s.GlobalVariablesRevision(workspace.Id)
	if err != nil || localRevision <= 0 {
		t.Fatalf("local revision = %d, err = %v", localRevision, err)
	}
	const remoteRevision = int64(42)
	if err := s.ApplySyncGlobalVariables(workspace.Id, []model.Variable{{Key: "region", Value: "remote", Enabled: true}}, remoteRevision); err != nil {
		t.Fatal(err)
	}
	revision, err := s.GlobalVariablesRevision(workspace.Id)
	if err != nil || revision != remoteRevision {
		t.Fatalf("applied revision = %d, err = %v", revision, err)
	}
	variables, err := s.GetGlobalVariables(workspace.Id)
	if err != nil || len(variables) != 1 || variables[0].Value != "remote" {
		t.Fatalf("applied globals = %+v, err = %v", variables, err)
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

	page, err := s.ListHistory(w.Id, model.HistoryQuery{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("len = %d, want 1", len(page.Items))
	}
	summary := page.Items[0]
	if summary.Url != "https://example.com/a" || summary.Method != "GET" || summary.Status != 200 || !summary.HasBody {
		t.Errorf("summary mismatch: %+v", summary)
	}
	detail, err := s.GetHistory(w.Id, id)
	if err != nil || detail.RequestSnap.Url != "https://example.com/a" ||
		detail.BodyInline != `{"ok":true}` || len(detail.TestResults) != 1 || detail.Timing.TotalMs != 12.5 {
		t.Errorf("detail mismatch: %+v, err = %v", detail, err)
	}

	// 搜索过滤
	page, _ = s.ListHistory(w.Id, model.HistoryQuery{Search: "example.com"})
	if len(page.Items) != 1 {
		t.Errorf("search hit = %d, want 1", len(page.Items))
	}
	page, _ = s.ListHistory(w.Id, model.HistoryQuery{Search: "nomatch-xyz"})
	if len(page.Items) != 0 {
		t.Errorf("search miss = %d, want 0", len(page.Items))
	}

	if err := s.ClearHistory(w.Id); err != nil {
		t.Fatalf("clear: %v", err)
	}
	page, _ = s.ListHistory(w.Id, model.HistoryQuery{})
	if len(page.Items) != 0 {
		t.Errorf("after clear len = %d, want 0", len(page.Items))
	}
}

func TestInsertHistoryRejectsDeletedWorkspace(t *testing.T) {
	s := openTestStore(t)
	workspace, _ := s.EnsureDefaultWorkspace()
	if err := s.DeleteWorkspace(workspace.Id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertHistory(model.HistoryDetail{
		WorkspaceId: workspace.Id,
		RequestSnap: model.HttpRequest{Method: "GET", Url: "https://example.test/orphan"},
	}); err == nil {
		t.Fatal("history was inserted after its workspace had been deleted")
	}
}

func TestInsertHistoryRejectsResponseBlob(t *testing.T) {
	s := openTestStore(t)
	workspace, _ := s.EnsureDefaultWorkspace()
	if _, err := s.InsertHistory(model.HistoryDetail{
		WorkspaceId: workspace.Id,
		RequestSnap: model.HttpRequest{Method: "GET", Url: "https://example.test"},
		BodyRef:     "raw-response.bin",
	}); err == nil {
		t.Fatal("history accepted a raw response blob")
	}
}

func TestHistoryCursorPaginationIsStable(t *testing.T) {
	s := openTestStore(t)
	w, _ := s.EnsureDefaultWorkspace()
	const createdAt = int64(123456789)
	for i := 0; i < 5; i++ {
		if _, err := s.InsertHistory(model.HistoryDetail{
			WorkspaceId: w.Id,
			RequestSnap: model.HttpRequest{Method: "GET", Url: fmt.Sprintf("https://example.com/%d", i)},
			CreatedAt:   createdAt,
		}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.ListHistory(w.Id, model.HistoryQuery{Limit: 2})
	if err != nil || len(first.Items) != 2 || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first page = %+v, err = %v", first, err)
	}
	second, err := s.ListHistory(w.Id, model.HistoryQuery{Limit: 2, Cursor: first.NextCursor})
	if err != nil || len(second.Items) != 2 || !second.HasMore {
		t.Fatalf("second page = %+v, err = %v", second, err)
	}
	third, err := s.ListHistory(w.Id, model.HistoryQuery{Limit: 2, Cursor: second.NextCursor})
	if err != nil || len(third.Items) != 1 || third.HasMore {
		t.Fatalf("third page = %+v, err = %v", third, err)
	}
	seen := map[string]bool{}
	for _, historyPage := range []model.HistoryPage{first, second, third} {
		for _, item := range historyPage.Items {
			if seen[item.Id] {
				t.Fatalf("duplicate history id %s", item.Id)
			}
			seen[item.Id] = true
		}
	}
	if len(seen) != 5 {
		t.Fatalf("seen = %d, want 5", len(seen))
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
	insertLegacyHistoryBlob(t, s, w.Id, ref)

	if err := s.ClearHistory(w.Id); err != nil {
		t.Fatalf("clear history: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("blob still exists, stat error = %v", err)
	}
}

func TestInsertHistoryPrunesOldestWorkspaceRowsAndBlobs(t *testing.T) {
	s := openTestStore(t)
	workspace, _ := s.EnsureDefaultWorkspace()
	otherWorkspace, _ := s.CreateWorkspace("other")
	const createdAt = int64(123456789)
	const prunedRef = "pruned-history.bin"
	prunedPath := filepath.Join(s.BlobsDir(), prunedRef)
	if err := os.WriteFile(prunedPath, []byte("prune me"), 0o600); err != nil {
		t.Fatal(err)
	}

	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < historyRetentionLimit; i++ {
		ref := ""
		if i == 0 {
			ref = prunedRef
		}
		if _, err := tx.Exec(`
			INSERT INTO history (id, workspace_id, request_snap, method, url, response_meta, body_ref, created_at)
			VALUES (?, ?, '{}', 'GET', ?, '{}', NULLIF(?, ''), ?)`,
			fmt.Sprintf("h%04d", i), workspace.Id, fmt.Sprintf("https://example.com/%d", i), ref, createdAt); err != nil {
			tx.Rollback()
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(`
		INSERT INTO history (id, workspace_id, request_snap, method, url, response_meta, created_at)
		VALUES ('other-history', ?, '{}', 'GET', 'https://other.example', '{}', ?)`,
		otherWorkspace.Id, createdAt-1); err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if _, err := s.InsertHistory(model.HistoryDetail{
		Id:          "zz-new-history",
		WorkspaceId: workspace.Id,
		RequestSnap: model.HttpRequest{Method: "POST", Url: "https://example.com/new"},
		CreatedAt:   createdAt,
	}); err != nil {
		t.Fatal(err)
	}

	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM history WHERE workspace_id = ?", workspace.Id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != historyRetentionLimit {
		t.Fatalf("workspace history count = %d, want %d", count, historyRetentionLimit)
	}
	if err := s.db.QueryRow("SELECT COUNT(*) FROM history WHERE workspace_id = ?", otherWorkspace.Id).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("other workspace history count = %d, want 1", count)
	}
	if _, err := s.GetHistory(workspace.Id, "h0000"); err == nil {
		t.Fatal("oldest history row was retained")
	}
	if _, err := s.GetHistory(workspace.Id, "zz-new-history"); err != nil {
		t.Fatalf("newest history row was pruned: %v", err)
	}
	if _, err := os.Stat(prunedPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pruned blob still exists: %v", err)
	}
}

func TestOpenRemovesOrphanedResponseBlobs(t *testing.T) {
	dir := t.TempDir()
	store, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := store.EnsureDefaultWorkspace()
	const referencedRef = "referenced.bin"
	insertLegacyHistoryBlob(t, store, workspace.Id, referencedRef)
	const orphanedRef = "11111111-1111-4111-8111-111111111111.bin"
	const freshOrphanRef = "22222222-2222-4222-8222-222222222222.bin"
	for name, content := range map[string]string{
		referencedRef:         "keep me",
		orphanedRef:           "remove me",
		freshOrphanRef:        "still in flight",
		".response-stale.tmp": "remove temp",
		"unmanaged.bin":       "leave unrelated file",
	} {
		if err := os.WriteFile(filepath.Join(store.BlobsDir(), name), []byte(content), 0o600); err != nil {
			store.Close()
			t.Fatal(err)
		}
	}
	staleTime := time.Now().Add(-25 * time.Hour)
	for _, name := range []string{orphanedRef, ".response-stale.tmp"} {
		if err := os.Chtimes(filepath.Join(store.BlobsDir(), name), staleTime, staleTime); err != nil {
			store.Close()
			t.Fatal(err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := os.Stat(filepath.Join(reopened.BlobsDir(), referencedRef)); err != nil {
		t.Fatalf("referenced blob was removed: %v", err)
	}
	for _, name := range []string{orphanedRef, ".response-stale.tmp"} {
		if _, err := os.Stat(filepath.Join(reopened.BlobsDir(), name)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("orphan %q still exists: %v", name, err)
		}
	}
	for _, name := range []string{freshOrphanRef, "unmanaged.bin"} {
		if _, err := os.Stat(filepath.Join(reopened.BlobsDir(), name)); err != nil {
			t.Fatalf("protected file %q was removed: %v", name, err)
		}
	}
}

func TestClearHistoryPreservesSharedBlobReference(t *testing.T) {
	s := openTestStore(t)
	firstWorkspace, _ := s.EnsureDefaultWorkspace()
	secondWorkspace, _ := s.CreateWorkspace("second")
	const ref = "shared-history.bin"
	path := filepath.Join(s.BlobsDir(), ref)
	if err := os.WriteFile(path, []byte("shared"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, workspaceId := range []string{firstWorkspace.Id, secondWorkspace.Id} {
		insertLegacyHistoryBlob(t, s, workspaceId, ref)
	}
	if err := s.ClearHistory(firstWorkspace.Id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("shared blob was removed: %v", err)
	}
	if err := s.ClearHistory(secondWorkspace.Id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unreferenced shared blob still exists: %v", err)
	}
}

func TestBlobMetadataRangeAndStreamingCopy(t *testing.T) {
	s := openTestStore(t)
	if err := os.MkdirAll(s.BlobsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	const ref = "range-test.bin"
	source := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
	if err := os.WriteFile(filepath.Join(s.BlobsDir(), ref), source, 0o600); err != nil {
		t.Fatal(err)
	}
	size, err := s.BlobInfo(ref)
	if err != nil || size != int64(len(source)) {
		t.Fatalf("size = %d, err = %v", size, err)
	}
	first, eof, err := s.ReadBlobRange(ref, 0, 10)
	if err != nil || eof || string(first) != "0123456789" {
		t.Fatalf("first = %q, eof=%v, err=%v", first, eof, err)
	}
	second, eof, err := s.ReadBlobRange(ref, 10, int64(len(source)))
	if err != nil || !eof || string(append(first, second...)) != string(source) {
		t.Fatalf("combined = %q, eof=%v, err=%v", append(first, second...), eof, err)
	}
	if _, _, err := s.ReadBlobRange(ref, -1, 1); err == nil {
		t.Fatal("negative offset was accepted")
	}
	if _, _, err := s.ReadBlobRange(ref, 0, (1<<20)+1); err == nil {
		t.Fatal("oversized range was accepted")
	}
	if _, err := s.BlobInfo("../escape"); err == nil {
		t.Fatal("path traversal ref was accepted")
	}

	destination := filepath.Join(t.TempDir(), "saved.bin")
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	written, err := s.CopyBlob(ref, destination)
	if err != nil || written != int64(len(source)) {
		t.Fatalf("written = %d, err = %v", written, err)
	}
	saved, err := os.ReadFile(destination)
	if err != nil || string(saved) != string(source) {
		t.Fatalf("saved = %q, err = %v", saved, err)
	}
}

func TestReadBlobRejectsOversizedFiles(t *testing.T) {
	s := openTestStore(t)
	const ref = "oversized.bin"
	file, err := os.Create(filepath.Join(s.BlobsDir(), ref))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate((32 << 20) + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadBlob(ref); err == nil {
		t.Fatal("oversized blob was loaded")
	}
}

func TestReplaceFileRestoresOriginalWhenCommitFails(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "destination.bin")
	tempPath := filepath.Join(dir, "new.tmp")
	if err := os.WriteFile(destination, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tempPath, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	injected := errors.New("injected rename failure")
	err := replaceFile(tempPath, destination, func(oldPath, newPath string) error {
		calls++
		if calls == 2 {
			return injected
		}
		return os.Rename(oldPath, newPath)
	})
	if !errors.Is(err, injected) {
		t.Fatalf("replace error = %v", err)
	}
	data, readErr := os.ReadFile(destination)
	if readErr != nil || string(data) != "original" {
		t.Fatalf("original destination was not restored: data=%q err=%v", data, readErr)
	}
}
