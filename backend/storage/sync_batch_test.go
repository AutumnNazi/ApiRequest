package storage

import (
	"testing"

	"apirequest/backend/model"
)

func TestApplySyncSnapshotRollsBackDatabaseAndVaultAsOneBatch(t *testing.T) {
	adapter := &memoryKeyring{}
	store := openStoreWithMemoryKeyring(t, t.TempDir(), adapter)
	defer store.Close()
	workspace, err := store.EnsureDefaultWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`
		CREATE TRIGGER reject_second_sync_node
		BEFORE INSERT ON node
		WHEN NEW.name = 'reject-sync-node'
		BEGIN
			SELECT RAISE(ABORT, 'injected sync failure');
		END`); err != nil {
		t.Fatal(err)
	}

	err = store.ApplySyncSnapshot(SyncSnapshotWrite{
		WorkspaceId:   workspace.Id,
		WorkspaceName: workspace.Name,
		Nodes: []SyncNodeRow{
			{Node: model.Node{
				Id: "first-sync-node", WorkspaceId: workspace.Id, Kind: "collection", Name: "first",
				Auth:      &model.Auth{Type: "bearer", Params: map[string]string{"token": "must-rollback"}},
				CreatedAt: 1, UpdatedAt: 1,
			}},
			{Node: model.Node{
				Id: "second-sync-node", WorkspaceId: workspace.Id, Kind: "collection", Name: "reject-sync-node",
				CreatedAt: 1, UpdatedAt: 1,
			}},
		},
		Environments: []model.Environment{{
			Id: "sync-environment", WorkspaceId: workspace.Id, Name: "sync", CreatedAt: 1, UpdatedAt: 1,
		}},
		Globals:         []model.Variable{{Key: "token", Value: "global-secret", Type: "secret", Enabled: true}},
		GlobalsRevision: 1,
	})
	if err == nil {
		t.Fatal("injected sync failure was not returned")
	}

	var nodeCount, environmentCount, globalCount int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM node WHERE workspace_id = ?", workspace.Id).Scan(&nodeCount); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow("SELECT COUNT(*) FROM environment WHERE workspace_id = ?", workspace.Id).Scan(&environmentCount); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow("SELECT COUNT(*) FROM global_var WHERE workspace_id = ?", workspace.Id).Scan(&globalCount); err != nil {
		t.Fatal(err)
	}
	if nodeCount != 0 || environmentCount != 0 || globalCount != 0 {
		t.Fatalf("partial sync remained: nodes=%d environments=%d globals=%d", nodeCount, environmentCount, globalCount)
	}
	if len(adapter.values) != 0 {
		t.Fatalf("sync Vault writes survived rollback: %+v", adapter.values)
	}
}
