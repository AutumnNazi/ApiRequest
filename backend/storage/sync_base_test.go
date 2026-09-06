package storage

import (
	"strings"
	"testing"
)

func TestSyncBaseRoundTrip(t *testing.T) {
	s := openStoreWithMemoryKeyring(t, t.TempDir(), &memoryKeyring{values: map[string]string{}})
	ws, err := s.EnsureDefaultWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := s.GetSyncBase(ws.Id); ok || err != nil {
		t.Fatalf("empty base: ok=%v err=%v", ok, err)
	}
	if err := s.PutSyncBase(ws.Id, 2, `{"nodes":[]}`); err != nil {
		t.Fatalf("PutSyncBase: %v", err)
	}
	version, snapshot, ok, err := s.GetSyncBase(ws.Id)
	if err != nil || !ok {
		t.Fatalf("GetSyncBase: ok=%v err=%v", ok, err)
	}
	if version != 2 || snapshot != `{"nodes":[]}` {
		t.Fatalf("base = v%d %s", version, snapshot)
	}
	// upsert 覆盖
	if err := s.PutSyncBase(ws.Id, 3, `{"nodes":["x"]}`); err != nil {
		t.Fatal(err)
	}
	version, snapshot, _, _ = s.GetSyncBase(ws.Id)
	if version != 3 || snapshot != `{"nodes":["x"]}` {
		t.Fatalf("after upsert = v%d %s", version, snapshot)
	}
	if err := s.DeleteSyncBase(ws.Id); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, _ := s.GetSyncBase(ws.Id); ok {
		t.Fatal("base survived delete")
	}
}

func TestSyncBaseCascadeOnWorkspaceDelete(t *testing.T) {
	s := openStoreWithMemoryKeyring(t, t.TempDir(), &memoryKeyring{values: map[string]string{}})
	ws, _ := s.EnsureDefaultWorkspace()
	if err := s.PutSyncBase(ws.Id, 2, `{"nodes":"`+strings.Repeat("x", 8)+`"}`); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteWorkspace(ws.Id); err != nil {
		t.Fatalf("DeleteWorkspace: %v", err)
	}
	if _, _, ok, _ := s.GetSyncBase(ws.Id); ok {
		t.Fatal("sync_base must cascade with workspace")
	}
}
