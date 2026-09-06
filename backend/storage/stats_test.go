package storage

import (
	"testing"

	"apirequest/backend/model"
)

func TestStorageStats(t *testing.T) {
	s := openStoreWithMemoryKeyring(t, t.TempDir(), &memoryKeyring{values: map[string]string{}})
	ws, _ := s.EnsureDefaultWorkspace()
	col, err := s.UpsertNode(model.Node{WorkspaceId: ws.Id, Kind: "collection", Name: "c"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertNode(model.Node{WorkspaceId: ws.Id, ParentId: col.Id, Kind: "request", Name: "r"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertExample(model.Example{NodeId: col.Id, Name: "ex", Status: 200}); err != nil {
		t.Fatal(err)
	}

	stats, err := s.StorageStats()
	if err != nil {
		t.Fatalf("StorageStats: %v", err)
	}
	if stats.Workspaces < 1 || stats.Nodes < 2 || stats.Examples < 1 {
		t.Fatalf("stats = %+v", stats)
	}
	if stats.DbBytes <= 0 || stats.WalBytes < 0 {
		t.Fatalf("sizes = db:%d wal:%d", stats.DbBytes, stats.WalBytes)
	}
}

func TestVacuumRuns(t *testing.T) {
	s := openStoreWithMemoryKeyring(t, t.TempDir(), &memoryKeyring{values: map[string]string{}})
	if _, err := s.EnsureDefaultWorkspace(); err != nil {
		t.Fatal(err)
	}
	if err := s.Vacuum(); err != nil {
		t.Fatalf("Vacuum: %v", err)
	}
}
