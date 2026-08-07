package binding

import (
	"encoding/json"
	"strings"
	"testing"

	"apirequest/backend/model"
	"apirequest/backend/secrets"
	"apirequest/backend/storage"
)

func TestListNodesReturnsSummariesWithoutUnlockingVault(t *testing.T) {
	dir := t.TempDir()
	vault := secrets.NewWithKeyring(dir, nil)
	if err := vault.Unlock("node-summary-test"); err != nil {
		t.Fatal(err)
	}
	store, err := storage.OpenWithVault(dir, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	workspace, _ := store.EnsureDefaultWorkspace()
	collection, err := store.UpsertNode(model.Node{WorkspaceId: workspace.Id, Kind: "collection", Name: "secured"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.UpsertNode(model.Node{
		WorkspaceId: workspace.Id, ParentId: collection.Id, Kind: "request", Name: "private",
		Request: &model.HttpRequest{
			Method: "POST", Url: "https://example.test", Body: model.Body{Kind: "raw", Text: "private-body"},
			Auth:     model.Auth{Type: "bearer", Params: map[string]string{"token": "private-token"}},
			Settings: model.DefaultSettings(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	vault.Lock()

	nodes, err := NewNodeApi(store).ListNodes(workspace.Id)
	if err != nil {
		t.Fatalf("list summaries while Vault is locked: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("summaries = %d, want 2", len(nodes))
	}
	raw, err := json.Marshal(nodes)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{"private-body", "private-token", `"request":`, `"auth":`, `"variables":`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("node summary contains %q: %s", forbidden, text)
		}
	}
}
