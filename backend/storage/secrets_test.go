package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"apirequest/backend/model"
	"apirequest/backend/secrets"
)

type memoryKeyring struct {
	values      map[string]string
	unavailable bool
}

func (m *memoryKeyring) Set(_, account, value string) error {
	if m.unavailable {
		return errors.New("unavailable")
	}
	if m.values == nil {
		m.values = map[string]string{}
	}
	m.values[account] = value
	return nil
}

func (m *memoryKeyring) Get(_, account string) (string, error) {
	if m.unavailable {
		return "", errors.New("unavailable")
	}
	value, ok := m.values[account]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return value, nil
}

func (m *memoryKeyring) Delete(_, account string) error {
	if m.unavailable {
		return errors.New("unavailable")
	}
	delete(m.values, account)
	return nil
}

func openStoreWithMemoryKeyring(t *testing.T, dir string, adapter *memoryKeyring) *Store {
	t.Helper()
	store, err := OpenWithVault(dir, secrets.NewWithKeyring(dir, adapter))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestCredentialsAreReferencesAtRestAndResolvedAtBoundary(t *testing.T) {
	dir := t.TempDir()
	store := openStoreWithMemoryKeyring(t, dir, &memoryKeyring{})
	workspace, _ := store.EnsureDefaultWorkspace()
	node, err := store.UpsertNode(model.Node{
		WorkspaceId: workspace.Id,
		Kind:        "collection",
		Name:        "secured",
		Auth: &model.Auth{Type: "basic", Params: map[string]string{
			"username": "alice",
			"password": "node-password",
		}},
		Variables: []model.Variable{{Key: "apiKey", Value: "node-variable-secret", Type: "secret", Enabled: true}},
		Request:   &model.HttpRequest{Auth: model.Auth{Type: "bearer", Params: map[string]string{"token": "request-token"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	environment, err := store.UpsertEnvironment(model.Environment{
		WorkspaceId: workspace.Id,
		Name:        "prod",
		Variables:   []model.Variable{{Key: "password", Value: "environment-secret", Type: "secret", Enabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetGlobalVariables(workspace.Id, []model.Variable{{Key: "token", Value: "global-secret", Type: "secret", Enabled: true}}); err != nil {
		t.Fatal(err)
	}

	var requestRaw, authRaw, nodeVarsRaw, envVarsRaw, globalVarsRaw string
	if err := store.db.QueryRow("SELECT request_data, auth, variables FROM node WHERE id = ?", node.Id).Scan(&requestRaw, &authRaw, &nodeVarsRaw); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow("SELECT variables FROM environment WHERE id = ?", environment.Id).Scan(&envVarsRaw); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow("SELECT variables FROM global_var WHERE workspace_id = ?", workspace.Id).Scan(&globalVarsRaw); err != nil {
		t.Fatal(err)
	}
	allRaw := strings.Join([]string{requestRaw, authRaw, nodeVarsRaw, envVarsRaw, globalVarsRaw}, "\n")
	for _, plaintext := range []string{"request-token", "node-password", "node-variable-secret", "environment-secret", "global-secret"} {
		if strings.Contains(allRaw, plaintext) {
			t.Fatalf("database contains plaintext %q: %s", plaintext, allRaw)
		}
	}
	if count := strings.Count(allRaw, "secret://keyring/"); count != 5 {
		t.Fatalf("reference count = %d, raw = %s", count, allRaw)
	}

	nodes, err := store.ListNodes(workspace.Id)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("nodes = %+v, err = %v", nodes, err)
	}
	got := nodes[0]
	if got.Request.Auth.Params["token"] != "request-token" || got.Auth.Params["password"] != "node-password" || got.Variables[0].Value != "node-variable-secret" {
		t.Fatalf("resolved node = %+v", got)
	}
	gotEnv, err := store.GetEnvironment(environment.Id)
	if err != nil || gotEnv.Variables[0].Value != "environment-secret" {
		t.Fatalf("resolved environment = %+v, err = %v", gotEnv, err)
	}
	globals, err := store.GetGlobalVariables(workspace.Id)
	if err != nil || globals[0].Value != "global-secret" {
		t.Fatalf("resolved globals = %+v, err = %v", globals, err)
	}
}

func TestFailedDatabaseWriteRollsBackSecretChanges(t *testing.T) {
	adapter := &memoryKeyring{}
	store := openStoreWithMemoryKeyring(t, t.TempDir(), adapter)
	workspace, _ := store.EnsureDefaultWorkspace()
	node, err := store.UpsertNode(model.Node{
		WorkspaceId: workspace.Id,
		Kind:        "collection",
		Name:        "secured",
		Auth:        modelAuth("old-password"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(adapter.values) != 1 {
		t.Fatalf("initial keyring entries = %d", len(adapter.values))
	}
	if _, err := store.db.Exec(`
		CREATE TRIGGER fail_node_secret_update
		BEFORE UPDATE ON node
		BEGIN
		  SELECT RAISE(ABORT, 'forced node update failure');
		END`); err != nil {
		t.Fatal(err)
	}

	node.Auth.Params["password"] = "new-password"
	node.Variables = []model.Variable{{Key: "token", Value: "new-variable", Type: "secret", Enabled: true}}
	if _, err := store.UpsertNode(node); err == nil {
		t.Fatal("forced database failure was ignored")
	}
	if len(adapter.values) != 1 {
		t.Fatalf("rollback left %d keyring entries, want 1", len(adapter.values))
	}
	nodes, err := store.ListNodes(workspace.Id)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("nodes = %+v, err = %v", nodes, err)
	}
	if got := nodes[0].Auth.Params["password"]; got != "old-password" {
		t.Fatalf("failed update changed stored password to %q", got)
	}
	if len(nodes[0].Variables) != 0 {
		t.Fatalf("failed update persisted variables: %+v", nodes[0].Variables)
	}
}

func TestRemovedSecretReferencesAreCleanedAcrossStoredEntities(t *testing.T) {
	adapter := &memoryKeyring{}
	store := openStoreWithMemoryKeyring(t, t.TempDir(), adapter)
	workspace, _ := store.EnsureDefaultWorkspace()
	node, err := store.UpsertNode(model.Node{
		WorkspaceId: workspace.Id,
		Kind:        "collection",
		Name:        "secured",
		Auth:        modelAuth("node-password"),
		Request: &model.HttpRequest{Auth: model.Auth{
			Type: "bearer", Params: map[string]string{"token": "request-token"},
		}},
		Variables: []model.Variable{{Key: "token", Value: "node-variable", Type: "secret", Enabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	environment, err := store.UpsertEnvironment(model.Environment{
		WorkspaceId: workspace.Id,
		Name:        "prod",
		Variables:   []model.Variable{{Key: "password", Value: "environment-secret", Type: "secret", Enabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetGlobalVariables(workspace.Id, []model.Variable{{Key: "token", Value: "global-secret", Type: "secret", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if len(adapter.values) != 5 {
		t.Fatalf("initial keyring entries = %d, want 5", len(adapter.values))
	}

	node.Auth = &model.Auth{Type: "none"}
	node.Request.Auth = model.Auth{Type: "none"}
	node.Variables = nil
	if _, err := store.UpsertNode(node); err != nil {
		t.Fatal(err)
	}
	if len(adapter.values) != 2 {
		t.Fatalf("node cleanup left %d keyring entries, want 2", len(adapter.values))
	}
	environment.Variables = nil
	if _, err := store.UpsertEnvironment(environment); err != nil {
		t.Fatal(err)
	}
	if len(adapter.values) != 1 {
		t.Fatalf("environment cleanup left %d keyring entries, want 1", len(adapter.values))
	}
	if err := store.SetGlobalVariables(workspace.Id, nil); err != nil {
		t.Fatal(err)
	}
	if len(adapter.values) != 0 {
		t.Fatalf("global cleanup left keyring entries: %+v", adapter.values)
	}
}

func TestApplySyncRemovesStaleSecretReferences(t *testing.T) {
	adapter := &memoryKeyring{}
	store := openStoreWithMemoryKeyring(t, t.TempDir(), adapter)
	workspace, _ := store.EnsureDefaultWorkspace()
	node, err := store.UpsertNode(model.Node{
		WorkspaceId: workspace.Id,
		Kind:        "collection",
		Name:        "secured",
		Auth:        modelAuth("node-password"),
	})
	if err != nil {
		t.Fatal(err)
	}
	environment, err := store.UpsertEnvironment(model.Environment{
		WorkspaceId: workspace.Id,
		Name:        "prod",
		Variables:   []model.Variable{{Key: "token", Value: "environment-secret", Type: "secret", Enabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(adapter.values) != 2 {
		t.Fatalf("initial keyring entries = %d, want 2", len(adapter.values))
	}

	node.Auth = &model.Auth{Type: "none"}
	if err := store.ApplySyncNode(SyncNodeRow{Node: node}); err != nil {
		t.Fatal(err)
	}
	environment.Variables = nil
	if err := store.ApplySyncEnvironment(environment); err != nil {
		t.Fatal(err)
	}
	if len(adapter.values) != 0 {
		t.Fatalf("sync apply left keyring entries: %+v", adapter.values)
	}
}

func TestReferenceLikeTypedCredentialsAreStoredAsPlaintext(t *testing.T) {
	adapter := &memoryKeyring{}
	store := openStoreWithMemoryKeyring(t, t.TempDir(), adapter)
	workspace, _ := store.EnsureDefaultWorkspace()
	const literal = "secret://file/literal-user-value"
	node, err := store.UpsertNode(model.Node{
		WorkspaceId: workspace.Id,
		Kind:        "collection",
		Name:        "literal",
		Auth:        modelAuth(literal),
	})
	if err != nil {
		t.Fatal(err)
	}
	environment, err := store.UpsertEnvironment(model.Environment{
		WorkspaceId: workspace.Id,
		Name:        "literal",
		Variables:   []model.Variable{{Key: "token", Value: literal, Type: "secret", Enabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetGlobalVariables(workspace.Id, []model.Variable{{Key: "token", Value: literal, Type: "secret", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if len(adapter.values) != 3 {
		t.Fatalf("keyring entries = %d, want 3", len(adapter.values))
	}
	var authRaw, environmentRaw, globalRaw string
	if err := store.db.QueryRow("SELECT auth FROM node WHERE id = ?", node.Id).Scan(&authRaw); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow("SELECT variables FROM environment WHERE id = ?", environment.Id).Scan(&environmentRaw); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow("SELECT variables FROM global_var WHERE workspace_id = ?", workspace.Id).Scan(&globalRaw); err != nil {
		t.Fatal(err)
	}
	allRaw := authRaw + environmentRaw + globalRaw
	if strings.Contains(allRaw, literal) || strings.Count(allRaw, "secret://keyring/") != 3 {
		t.Fatalf("unsafe reference-like credentials at rest: %s", allRaw)
	}
	nodes, err := store.ListNodes(workspace.Id)
	if err != nil || nodes[0].Auth.Params["password"] != literal {
		t.Fatalf("node literal = %+v, err = %v", nodes, err)
	}
	resolvedEnvironment, err := store.GetEnvironment(environment.Id)
	if err != nil || resolvedEnvironment.Variables[0].Value != literal {
		t.Fatalf("environment literal = %+v, err = %v", resolvedEnvironment, err)
	}
	globals, err := store.GetGlobalVariables(workspace.Id)
	if err != nil || globals[0].Value != literal {
		t.Fatalf("global literal = %+v, err = %v", globals, err)
	}
}

func TestHardDeleteCleansSecretsAndDatabaseFailureRestoresThem(t *testing.T) {
	adapter := &memoryKeyring{}
	store := openStoreWithMemoryKeyring(t, t.TempDir(), adapter)
	firstWorkspace, _ := store.EnsureDefaultWorkspace()
	secondWorkspace, err := store.CreateWorkspace("second")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertNode(model.Node{
		WorkspaceId: firstWorkspace.Id, Kind: "collection", Name: "first", Auth: modelAuth("first-node-secret"),
	}); err != nil {
		t.Fatal(err)
	}
	firstEnvironment, err := store.UpsertEnvironment(model.Environment{
		WorkspaceId: firstWorkspace.Id,
		Name:        "prod",
		Variables:   []model.Variable{{Key: "token", Value: "first-environment-secret", Type: "secret", Enabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetGlobalVariables(firstWorkspace.Id, []model.Variable{{Key: "token", Value: "first-global-secret", Type: "secret", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	secondNode, err := store.UpsertNode(model.Node{
		WorkspaceId: secondWorkspace.Id, Kind: "collection", Name: "second", Auth: modelAuth("second-node-secret"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(adapter.values) != 4 {
		t.Fatalf("initial keyring entries = %d, want 4", len(adapter.values))
	}
	if _, err := store.db.Exec(`
		CREATE TRIGGER fail_workspace_node_delete
		BEFORE DELETE ON node
		BEGIN SELECT RAISE(ABORT, 'forced workspace delete failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteWorkspace(firstWorkspace.Id); err == nil {
		t.Fatal("forced workspace delete failure was ignored")
	}
	if len(adapter.values) != 4 {
		t.Fatalf("failed workspace delete damaged keyring: %+v", adapter.values)
	}
	if _, err := store.GetEnvironment(firstEnvironment.Id); err != nil {
		t.Fatalf("failed workspace delete removed environment: %v", err)
	}
	if _, err := store.db.Exec("DROP TRIGGER fail_workspace_node_delete"); err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteEnvironment(firstEnvironment.Id); err != nil {
		t.Fatal(err)
	}
	if len(adapter.values) != 3 {
		t.Fatalf("environment delete left %d keyring entries, want 3", len(adapter.values))
	}
	if err := store.DeleteWorkspace(firstWorkspace.Id); err != nil {
		t.Fatal(err)
	}
	if len(adapter.values) != 1 {
		t.Fatalf("workspace delete left keyring entries: %+v", adapter.values)
	}
	nodes, err := store.ListNodes(secondWorkspace.Id)
	if err != nil || len(nodes) != 1 || nodes[0].Id != secondNode.Id || nodes[0].Auth.Params["password"] != "second-node-secret" {
		t.Fatalf("unrelated workspace secret was damaged: %+v, %v", nodes, err)
	}
}

func TestConcurrentSecretWritesKeepMetadataAndVaultValueTogether(t *testing.T) {
	store := openStoreWithMemoryKeyring(t, t.TempDir(), &memoryKeyring{})
	workspace, _ := store.EnsureDefaultWorkspace()
	base, err := store.UpsertNode(model.Node{
		WorkspaceId: workspace.Id, Kind: "collection", Name: "initial", Auth: modelAuth("initial"),
	})
	if err != nil {
		t.Fatal(err)
	}

	const writers = 32
	start := make(chan struct{})
	errors := make(chan error, writers)
	var wait sync.WaitGroup
	for i := 0; i < writers; i++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			label := fmt.Sprintf("writer-%02d", index)
			candidate := base
			candidate.Name = label
			candidate.Auth = modelAuth(label)
			_, err := store.UpsertNode(candidate)
			errors <- err
		}(i)
	}
	close(start)
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	nodes, err := store.ListNodes(workspace.Id)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("nodes = %+v, err = %v", nodes, err)
	}
	if password := nodes[0].Auth.Params["password"]; password != nodes[0].Name {
		t.Fatalf("database metadata %q points at Vault value %q", nodes[0].Name, password)
	}
}

func modelAuth(password string) *model.Auth {
	return &model.Auth{Type: "basic", Params: map[string]string{
		"username": "alice",
		"password": password,
	}}
}

func TestLegacyPlaintextCredentialsMigrateOnReopen(t *testing.T) {
	dir := t.TempDir()
	lockedAdapter := &memoryKeyring{unavailable: true}
	lockedStore, err := OpenWithVault(dir, secrets.NewWithKeyring(dir, lockedAdapter))
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := lockedStore.EnsureDefaultWorkspace()
	_, err = lockedStore.db.Exec(`INSERT INTO node
		(id, workspace_id, kind, name, sort_order, request_data, auth, variables, created_at, updated_at)
		VALUES ('legacy', ?, 'collection', 'legacy', 0,
		'{"auth":{"type":"bearer","params":{"token":"legacy-token"}}}',
		'{"type":"basic","params":{"password":"legacy-password"}}',
		'[{"key":"secret","value":"legacy-variable","type":"secret","enabled":true}]', 1, 1)`, workspace.Id)
	if err != nil {
		t.Fatal(err)
	}
	if err := lockedStore.Close(); err != nil {
		t.Fatal(err)
	}

	adapter := &memoryKeyring{}
	migrated := openStoreWithMemoryKeyring(t, dir, adapter)
	var requestRaw, authRaw, variablesRaw string
	if err := migrated.db.QueryRow("SELECT request_data, auth, variables FROM node WHERE id = 'legacy'").Scan(&requestRaw, &authRaw, &variablesRaw); err != nil {
		t.Fatal(err)
	}
	allRaw := requestRaw + authRaw + variablesRaw
	for _, plaintext := range []string{"legacy-token", "legacy-password", "legacy-variable"} {
		if strings.Contains(allRaw, plaintext) {
			t.Fatalf("legacy plaintext %q survived migration: %s", plaintext, allRaw)
		}
	}
	if strings.Count(allRaw, "secret://keyring/") != 3 {
		t.Fatalf("migrated raw = %s", allRaw)
	}
	nodes, err := migrated.ListNodes(workspace.Id)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("nodes = %+v, err = %v", nodes, err)
	}
	if nodes[0].Request.Auth.Params["token"] != "legacy-token" || nodes[0].Auth.Params["password"] != "legacy-password" || nodes[0].Variables[0].Value != "legacy-variable" {
		t.Fatalf("migrated node did not resolve: %+v", nodes[0])
	}
}

func TestHistoryCredentialsAreIrreversiblyRedacted(t *testing.T) {
	store := openStoreWithMemoryKeyring(t, t.TempDir(), &memoryKeyring{})
	workspace, _ := store.EnsureDefaultWorkspace()
	if _, err := store.Vault().Put("test/history-variable", "vault-history-secret"); err != nil {
		t.Fatal(err)
	}
	_, err := store.InsertHistory(model.HistoryItem{
		WorkspaceId: workspace.Id,
		RequestSnap: model.HttpRequest{
			Url:  "https://example.test/vault-history-secret",
			Body: model.Body{Kind: "raw", Text: `{"token":"vault-history-secret"}`},
			Auth: model.Auth{Type: "bearer", Params: map[string]string{"token": "history-token"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := store.db.QueryRow("SELECT request_snap FROM history LIMIT 1").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var snapshot model.HttpRequest
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "history-token") || strings.Contains(raw, "vault-history-secret") || snapshot.Auth.Params["token"] != "<redacted>" {
		t.Fatalf("unsafe history snapshot: %s", raw)
	}
}
