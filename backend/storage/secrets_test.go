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
		Request: &model.HttpRequest{
			Auth: model.Auth{Type: "bearer", Params: map[string]string{"token": "request-token"}},
			Params: []model.KV{
				{Key: "api_key", Value: "query-api-key", Enabled: true},
				{Key: "page", Value: "1", Enabled: true},
			},
			Headers: []model.KV{
				{Key: "Authorization", Value: "Bearer manual-header", Enabled: true},
				{Key: "X-Trace", Value: "public-trace", Enabled: true},
			},
			Body: model.Body{Kind: "urlencoded", Items: []model.FormItem{
				{Key: "password", Type: "text", Value: "form-password", Enabled: true},
			}},
		},
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
	for _, plaintext := range []string{"request-token", "query-api-key", "Bearer manual-header", "form-password", "node-password", "node-variable-secret", "environment-secret", "global-secret"} {
		if strings.Contains(allRaw, plaintext) {
			t.Fatalf("database contains plaintext %q: %s", plaintext, allRaw)
		}
	}
	if count := strings.Count(allRaw, "secret://keyring/"); count != 8 {
		t.Fatalf("reference count = %d, raw = %s", count, allRaw)
	}

	nodes, err := store.ListNodes(workspace.Id)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("nodes = %+v, err = %v", nodes, err)
	}
	got := nodes[0]
	if got.Request.Auth.Params["token"] != "request-token" || got.Request.Params[0].Value != "query-api-key" ||
		got.Request.Headers[0].Value != "Bearer manual-header" || got.Request.Body.Items[0].Value != "form-password" ||
		got.Auth.Params["password"] != "node-password" || got.Variables[0].Value != "node-variable-secret" {
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
		Request: &model.HttpRequest{
			Auth:    model.Auth{Type: "bearer", Params: map[string]string{"token": "request-token"}},
			Headers: []model.KV{{Key: "Authorization", Value: "Bearer header-token", Enabled: true}},
		},
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
	if len(adapter.values) != 6 {
		t.Fatalf("initial keyring entries = %d, want 6", len(adapter.values))
	}

	node.Auth = &model.Auth{Type: "none"}
	node.Request.Auth = model.Auth{Type: "none"}
	node.Request.Headers = nil
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

func TestSecretMigrationMarkerFailureIsSafelyRetryable(t *testing.T) {
	dir := t.TempDir()
	adapter := &memoryKeyring{}
	store := openStoreWithMemoryKeyring(t, dir, adapter)
	workspace, _ := store.EnsureDefaultWorkspace()
	authRaw, _ := json.Marshal(model.Auth{Type: "basic", Params: map[string]string{"password": "retry-password"}})
	if _, err := store.db.Exec(`INSERT INTO node
		(id, workspace_id, kind, name, sort_order, auth, created_at, updated_at)
		VALUES ('marker-retry-node', ?, 'collection', 'marker retry', 0, ?, 1, 1)`, workspace.Id, string(authRaw)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("DELETE FROM setting WHERE key IN (?, ?, ?, ?)", secretMigrationKey, headerSecretMigrationKey, requestValueMigrationKey, secretRefNormalizationKey); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`CREATE TRIGGER fail_secret_migration_marker
		BEFORE INSERT ON setting
		WHEN NEW.key = 'secrets.migration.v2'
		BEGIN SELECT RAISE(ABORT, 'forced secret marker failure'); END`); err != nil {
		t.Fatal(err)
	}

	if err := store.MigrateSecrets(); err == nil {
		t.Fatal("forced secret migration marker failure was ignored")
	}
	if marker, err := store.GetSetting(secretMigrationKey); err != nil || marker != "" {
		t.Fatalf("failed top-level marker = %q, err = %v", marker, err)
	}
	var firstAuth string
	if err := store.db.QueryRow("SELECT auth FROM node WHERE id = 'marker-retry-node'").Scan(&firstAuth); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(firstAuth, "retry-password") || !strings.Contains(firstAuth, "secret://keyring/") {
		t.Fatalf("first migration did not leave a recoverable reference: %s", firstAuth)
	}
	entryCount := len(adapter.values)
	if _, err := store.db.Exec("DROP TRIGGER fail_secret_migration_marker"); err != nil {
		t.Fatal(err)
	}
	if err := store.MigrateSecrets(); err != nil {
		t.Fatal(err)
	}
	var secondAuth string
	if err := store.db.QueryRow("SELECT auth FROM node WHERE id = 'marker-retry-node'").Scan(&secondAuth); err != nil {
		t.Fatal(err)
	}
	if secondAuth != firstAuth || len(adapter.values) != entryCount {
		t.Fatalf("retry changed migrated state: before=%s after=%s entries=%d/%d", firstAuth, secondAuth, entryCount, len(adapter.values))
	}
	node, err := store.GetNode(workspace.Id, "marker-retry-node")
	if err != nil || node.Auth == nil || node.Auth.Params["password"] != "retry-password" {
		t.Fatalf("retry result = %+v, err = %v", node.Auth, err)
	}
}

func TestHistoryCredentialsAreIrreversiblyRedacted(t *testing.T) {
	store := openStoreWithMemoryKeyring(t, t.TempDir(), &memoryKeyring{})
	workspace, _ := store.EnsureDefaultWorkspace()
	if _, err := store.Vault().Put("test/history-variable", "vault-history-secret"); err != nil {
		t.Fatal(err)
	}
	_, err := store.InsertHistory(model.HistoryRecord{
		WorkspaceId: workspace.Id,
		RequestSnap: model.HttpRequest{
			Url:     "https://example.test/vault-history-secret",
			Body:    model.Body{Kind: "raw", Text: `{"token":"vault-history-secret"}`},
			Auth:    model.Auth{Type: "bearer", Params: map[string]string{"token": "history-token"}},
			Headers: []model.KV{{Key: "Authorization", Value: "Bearer manual-history-secret", Enabled: true}},
			Params:  []model.KV{{Key: "api_token", Value: "history-query-secret", Enabled: true}},
		},
		BodyInline: `{"echo":"Bearer manual-history-secret history-query-secret"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	var raw, body string
	if err := store.db.QueryRow("SELECT request_snap, body_inline FROM history LIMIT 1").Scan(&raw, &body); err != nil {
		t.Fatal(err)
	}
	var snapshot model.HttpRequest
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw+body, "history-token") || strings.Contains(raw+body, "vault-history-secret") ||
		strings.Contains(raw+body, "manual-history-secret") || strings.Contains(raw+body, "history-query-secret") ||
		snapshot.Auth.Params["token"] != "<redacted>" || snapshot.Headers[0].Value != "<redacted>" ||
		snapshot.Params[0].Value != "<redacted>" {
		t.Fatalf("unsafe history snapshot: request=%s body=%s", raw, body)
	}
}

func TestHistoryRedactsShortAndOverlappingCredentialEchoes(t *testing.T) {
	store := openStoreWithMemoryKeyring(t, t.TempDir(), &memoryKeyring{})
	workspace, _ := store.EnsureDefaultWorkspace()
	id, err := store.InsertHistory(model.HistoryRecord{
		WorkspaceId: workspace.Id,
		RequestSnap: model.HttpRequest{Params: []model.KV{
			{Key: "token", Value: "abc", Enabled: true},
			{Key: "access_token", Value: "abcdef", Enabled: true},
			{Key: "api_key", Value: "x", Enabled: true},
		}},
		BodyInline:  "long=abcdef short=abc tiny=x",
		TestResults: []model.TestResult{{Name: "x", Error: "abcdef abc x"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var body, tests string
	if err := store.db.QueryRow("SELECT body_inline, test_results FROM history WHERE id = ?", id).Scan(&body, &tests); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body+tests, "abcdef") || strings.Contains(body+tests, "abc") ||
		body != "long=<redacted> short=<redacted> tiny=<redacted>" {
		t.Fatalf("short or overlapping credentials leaked: body=%q tests=%s", body, tests)
	}
}

func TestLegacyHeaderSecretsMigrateAfterOriginalSecretMarker(t *testing.T) {
	dir := t.TempDir()
	adapter := &memoryKeyring{}
	store := openStoreWithMemoryKeyring(t, dir, adapter)
	workspace, _ := store.EnsureDefaultWorkspace()
	request := model.HttpRequest{Headers: []model.KV{
		{Key: "Authorization", Value: "Bearer legacy-header", Enabled: true},
		{Key: "X-Trace", Value: "public", Enabled: true},
	}}
	raw, _ := json.Marshal(request)
	if _, err := store.db.Exec(`INSERT INTO node
		(id, workspace_id, kind, name, sort_order, request_data, created_at, updated_at)
		VALUES ('legacy-header-node', ?, 'request', 'legacy', 0, ?, 1, 1)`, workspace.Id, string(raw)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(secretMigrationKey, "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("DELETE FROM setting WHERE key = ?", headerSecretMigrationKey); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openStoreWithMemoryKeyring(t, dir, adapter)
	var stored string
	if err := reopened.db.QueryRow("SELECT request_data FROM node WHERE id = 'legacy-header-node'").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, "legacy-header") || !strings.Contains(stored, "secret://keyring/") {
		t.Fatalf("legacy header was not migrated: %s", stored)
	}
	got, err := reopened.GetNode(workspace.Id, "legacy-header-node")
	if err != nil || got.Request.Headers[0].Value != "Bearer legacy-header" || got.Request.Headers[1].Value != "public" {
		t.Fatalf("migrated node = %+v, err = %v", got, err)
	}
}

func TestOriginalSecretMigrationDoesNotDoubleWrapHeaderReferences(t *testing.T) {
	dir := t.TempDir()
	adapter := &memoryKeyring{}
	store := openStoreWithMemoryKeyring(t, dir, adapter)
	workspace, _ := store.EnsureDefaultWorkspace()
	request := model.HttpRequest{Headers: []model.KV{{Key: "Authorization", Value: "Bearer migrate-once", Enabled: true}}}
	raw, _ := json.Marshal(request)
	if _, err := store.db.Exec(`INSERT INTO node
		(id, workspace_id, kind, name, sort_order, request_data, created_at, updated_at)
		VALUES ('full-migration-header', ?, 'request', 'legacy', 0, ?, 1, 1)`, workspace.Id, string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("DELETE FROM setting WHERE key IN (?, ?)", secretMigrationKey, headerSecretMigrationKey); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openStoreWithMemoryKeyring(t, dir, adapter)
	got, err := reopened.GetNode(workspace.Id, "full-migration-header")
	if err != nil || got.Request.Headers[0].Value != "Bearer migrate-once" {
		t.Fatalf("migrated header = %+v, err = %v", got, err)
	}
	if len(adapter.values) != 1 {
		t.Fatalf("header migration wrote %d vault entries, want 1", len(adapter.values))
	}
}

func TestDedicatedHeaderMigrationPreservesExistingVaultReferences(t *testing.T) {
	dir := t.TempDir()
	adapter := &memoryKeyring{}
	store := openStoreWithMemoryKeyring(t, dir, adapter)
	workspace, _ := store.EnsureDefaultWorkspace()
	ref, err := store.Vault().Put("node/partial/request/header/authorization", "Bearer existing-ref")
	if err != nil {
		t.Fatal(err)
	}
	request := model.HttpRequest{Headers: []model.KV{{Key: "Authorization", Value: ref, Enabled: true}}}
	raw, _ := json.Marshal(request)
	if _, err := store.db.Exec(`INSERT INTO node
		(id, workspace_id, kind, name, sort_order, request_data, created_at, updated_at)
		VALUES ('partial-header-node', ?, 'request', 'partial', 0, ?, 1, 1)`, workspace.Id, string(raw)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(secretMigrationKey, "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("DELETE FROM setting WHERE key = ?", headerSecretMigrationKey); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openStoreWithMemoryKeyring(t, dir, adapter)
	got, err := reopened.GetNode(workspace.Id, "partial-header-node")
	if err != nil || got.Request.Headers[0].Value != "Bearer existing-ref" {
		t.Fatalf("preserved header = %+v, err = %v", got, err)
	}
	if len(adapter.values) != 1 {
		t.Fatalf("dedicated migration wrote %d vault entries, want 1", len(adapter.values))
	}
}

func TestDedicatedHeaderMigrationRollsBackVaultAndMarkerOnDatabaseFailure(t *testing.T) {
	dir := t.TempDir()
	adapter := &memoryKeyring{}
	store := openStoreWithMemoryKeyring(t, dir, adapter)
	workspace, _ := store.EnsureDefaultWorkspace()
	request := model.HttpRequest{Headers: []model.KV{{Key: "Authorization", Value: "Bearer rollback-header", Enabled: true}}}
	raw, _ := json.Marshal(request)
	if _, err := store.db.Exec(`INSERT INTO node
		(id, workspace_id, kind, name, sort_order, request_data, created_at, updated_at)
		VALUES ('rollback-header-node', ?, 'request', 'rollback', 0, ?, 1, 1)`, workspace.Id, string(raw)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(secretMigrationKey, "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("DELETE FROM setting WHERE key = ?", headerSecretMigrationKey); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`CREATE TRIGGER fail_header_migration
		BEFORE UPDATE OF request_data ON node
		BEGIN SELECT RAISE(ABORT, 'forced header migration failure'); END`); err != nil {
		t.Fatal(err)
	}

	if err := store.MigrateSecrets(); err == nil {
		t.Fatal("forced header migration failure was ignored")
	}
	if len(adapter.values) != 0 {
		t.Fatalf("failed migration left vault entries: %+v", adapter.values)
	}
	if marker, err := store.GetSetting(headerSecretMigrationKey); err != nil || marker != "" {
		t.Fatalf("failed migration marker = %q, err = %v", marker, err)
	}
	var stored string
	if err := store.db.QueryRow("SELECT request_data FROM node WHERE id = 'rollback-header-node'").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stored, "rollback-header") {
		t.Fatalf("failed migration changed request data: %s", stored)
	}
}

func TestLegacyStructuredRequestSecretsMigrateAfterOlderMarkers(t *testing.T) {
	dir := t.TempDir()
	adapter := &memoryKeyring{}
	store := openStoreWithMemoryKeyring(t, dir, adapter)
	workspace, _ := store.EnsureDefaultWorkspace()
	request := model.HttpRequest{
		Params: []model.KV{{Key: "access_token", Value: "legacy-query-token", Enabled: true}},
		Body: model.Body{Kind: "urlencoded", Items: []model.FormItem{{
			Key: "password", Type: "text", Value: "legacy-form-password", Enabled: false,
		}}},
	}
	raw, _ := json.Marshal(request)
	if _, err := store.db.Exec(`INSERT INTO node
		(id, workspace_id, kind, name, sort_order, request_data, created_at, updated_at)
		VALUES ('legacy-structured-node', ?, 'request', 'legacy structured', 0, ?, 1, 1)`, workspace.Id, string(raw)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(secretMigrationKey, "1"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(headerSecretMigrationKey, "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("DELETE FROM setting WHERE key = ?", requestValueMigrationKey); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openStoreWithMemoryKeyring(t, dir, adapter)
	var stored string
	if err := reopened.db.QueryRow("SELECT request_data FROM node WHERE id = 'legacy-structured-node'").Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, "legacy-query-token") || strings.Contains(stored, "legacy-form-password") ||
		strings.Count(stored, "secret://keyring/") != 2 {
		t.Fatalf("legacy structured values were not migrated: %s", stored)
	}
	got, err := reopened.GetNode(workspace.Id, "legacy-structured-node")
	if err != nil || got.Request.Params[0].Value != "legacy-query-token" || got.Request.Body.Items[0].Value != "legacy-form-password" {
		t.Fatalf("migrated structured request = %+v, err = %v", got, err)
	}
}

func TestLegacyStructuredReferenceLikeLiteralsAreMigratedAsPlaintext(t *testing.T) {
	dir := t.TempDir()
	adapter := &memoryKeyring{}
	store := openStoreWithMemoryKeyring(t, dir, adapter)
	workspace, _ := store.EnsureDefaultWorkspace()
	const literal = "secret://file/literal-structured-value"
	request := model.HttpRequest{
		Params: []model.KV{{Key: "access_token", Value: literal, Enabled: true}},
		Body: model.Body{Kind: "urlencoded", Items: []model.FormItem{{
			Key: "password", Type: "text", Value: literal, Enabled: false,
		}}},
	}
	raw, _ := json.Marshal(request)
	if _, err := store.db.Exec(`INSERT INTO node
		(id, workspace_id, kind, name, sort_order, request_data, created_at, updated_at)
		VALUES ('legacy-literal-node', ?, 'request', 'legacy literal', 0, ?, 1, 1)`, workspace.Id, string(raw)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(secretMigrationKey, "1"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting(headerSecretMigrationKey, "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("DELETE FROM setting WHERE key = ?", requestValueMigrationKey); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openStoreWithMemoryKeyring(t, dir, adapter)
	got, err := reopened.GetNode(workspace.Id, "legacy-literal-node")
	if err != nil {
		t.Fatal(err)
	}
	if got.Request.Params[0].Value != literal || got.Request.Body.Items[0].Value != literal {
		t.Fatalf("reference-like structured literals = %+v", got.Request)
	}
	if len(adapter.values) != 2 {
		t.Fatalf("structured literal vault entries = %d, want 2", len(adapter.values))
	}
}

func TestCanonicalReferenceLikeLiteralIsRekeyedAfterAmbiguousFileVaultUnlock(t *testing.T) {
	dir := t.TempDir()
	fileVault := secrets.NewWithKeyring(dir, nil)
	if err := fileVault.Unlock("canonical-literal-test"); err != nil {
		t.Fatal(err)
	}
	store, err := OpenWithVault(dir, fileVault)
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := store.EnsureDefaultWorkspace()
	const literal = "secret://file/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	request := model.HttpRequest{Params: []model.KV{{Key: "access_token", Value: literal, Enabled: true}}}
	raw, _ := json.Marshal(request)
	if _, err := store.db.Exec(`INSERT INTO node
		(id, workspace_id, kind, name, sort_order, request_data, created_at, updated_at)
		VALUES ('canonical-literal-node', ?, 'request', 'canonical literal', 0, ?, 1, 1)`, workspace.Id, string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("DELETE FROM setting WHERE key IN (?, ?, ?)", secretMigrationKey, headerSecretMigrationKey, requestValueMigrationKey); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	adapter := &memoryKeyring{}
	mixedVault := secrets.NewWithKeyring(dir, adapter)
	reopened, err := OpenWithVault(dir, mixedVault)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if marker, err := reopened.GetSetting(secretRefNormalizationKey); err != nil || marker != "" {
		t.Fatalf("ambiguous literal migration marker = %q, err = %v", marker, err)
	}
	if err := mixedVault.Unlock("canonical-literal-test"); err != nil {
		t.Fatal(err)
	}
	if err := reopened.MigrateSecrets(); err != nil {
		t.Fatal(err)
	}
	got, err := reopened.GetNode(workspace.Id, "canonical-literal-node")
	if err != nil || got.Request.Params[0].Value != literal {
		t.Fatalf("canonical reference-like literal = %+v, err = %v", got.Request, err)
	}
	if len(adapter.values) != 1 {
		t.Fatalf("canonical literal vault entries = %d, want 1", len(adapter.values))
	}
}

func TestMixedVaultWaitsForUnlockThenMovesLegacyFileReferenceToKeyring(t *testing.T) {
	dir := t.TempDir()
	fileVault := secrets.NewWithKeyring(dir, nil)
	if err := fileVault.Unlock("mixed-vault-test"); err != nil {
		t.Fatal(err)
	}
	fileStore, err := OpenWithVault(dir, fileVault)
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := fileStore.EnsureDefaultWorkspace()
	const secret = "legacy-file-password"
	legacyRef, err := fileVault.Put("legacy/node/password", secret)
	if err != nil {
		t.Fatal(err)
	}
	authRaw, _ := json.Marshal(model.Auth{Type: "basic", Params: map[string]string{"password": legacyRef}})
	if _, err := fileStore.db.Exec(`INSERT INTO node
		(id, workspace_id, kind, name, sort_order, auth, created_at, updated_at)
		VALUES ('mixed-vault-node', ?, 'collection', 'mixed vault', 0, ?, 1, 1)`, workspace.Id, string(authRaw)); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{secretMigrationKey, headerSecretMigrationKey, requestValueMigrationKey, cookieSecretMigrationKey} {
		if err := fileStore.SetSetting(key, "1"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := fileStore.db.Exec("DELETE FROM setting WHERE key = ?", secretRefNormalizationKey); err != nil {
		t.Fatal(err)
	}
	if err := fileStore.Close(); err != nil {
		t.Fatal(err)
	}

	adapter := &memoryKeyring{}
	mixedVault := secrets.NewWithKeyring(dir, adapter)
	mixedStore, err := OpenWithVault(dir, mixedVault)
	if err != nil {
		t.Fatal(err)
	}
	defer mixedStore.Close()
	var storedAuth string
	if err := mixedStore.db.QueryRow("SELECT auth FROM node WHERE id = 'mixed-vault-node'").Scan(&storedAuth); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(storedAuth, legacyRef) {
		t.Fatalf("locked legacy reference was rewritten before unlock: %s", storedAuth)
	}
	if marker, err := mixedStore.GetSetting(secretMigrationKey); err != nil || marker != "1" {
		t.Fatalf("existing mixed-vault marker = %q, err = %v", marker, err)
	}

	if err := mixedVault.Unlock("mixed-vault-test"); err != nil {
		t.Fatal(err)
	}
	if err := mixedStore.MigrateSecrets(); err != nil {
		t.Fatal(err)
	}
	if err := mixedStore.db.QueryRow("SELECT auth FROM node WHERE id = 'mixed-vault-node'").Scan(&storedAuth); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(storedAuth, legacyRef) || !strings.Contains(storedAuth, "secret://keyring/") {
		t.Fatalf("unlocked legacy reference was not moved to keyring: %s", storedAuth)
	}
	if value, err := mixedVault.Resolve(legacyRef); err != nil || value != secret {
		t.Fatalf("legacy file fallback was damaged: value=%q err=%v", value, err)
	}
	mixedVault.Lock()
	node, err := mixedStore.GetNode(workspace.Id, "mixed-vault-node")
	if err != nil || node.Auth == nil || node.Auth.Params["password"] != secret {
		t.Fatalf("migrated credential after file lock = %+v, err = %v", node.Auth, err)
	}
}

func TestMixedVaultPromotesSecretSettingsAfterUnlock(t *testing.T) {
	dir := t.TempDir()
	fileVault := secrets.NewWithKeyring(dir, nil)
	if err := fileVault.Unlock("settings-promotion"); err != nil {
		t.Fatal(err)
	}
	fileStore, err := OpenWithVault(dir, fileVault)
	if err != nil {
		t.Fatal(err)
	}
	proxyRef, err := fileVault.Put("setting/proxy/password", "proxy-file-secret")
	if err != nil {
		t.Fatal(err)
	}
	oauthRef, err := fileVault.Put("setting/oauth.token.fingerprint", `{"accessToken":"oauth-file-secret"}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := fileStore.SetSettings(map[string]string{
		"proxy.password":          proxyRef,
		"oauth.token.fingerprint": oauthRef,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := fileStore.db.Exec("DELETE FROM setting WHERE key = ?", secretRefNormalizationKey); err != nil {
		t.Fatal(err)
	}
	if err := fileStore.Close(); err != nil {
		t.Fatal(err)
	}

	adapter := &memoryKeyring{values: map[string]string{}}
	mixedVault := secrets.NewWithKeyring(dir, adapter)
	mixedStore, err := OpenWithVault(dir, mixedVault)
	if err != nil {
		t.Fatal(err)
	}
	defer mixedStore.Close()
	for _, key := range []string{"proxy.password", "oauth.token.fingerprint"} {
		value, err := mixedStore.GetSetting(key)
		if err != nil || !secrets.IsFileRef(value) {
			t.Fatalf("locked setting %q = %q, err = %v", key, value, err)
		}
	}

	if err := mixedVault.Unlock("settings-promotion"); err != nil {
		t.Fatal(err)
	}
	if err := mixedStore.MigrateSecrets(); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"proxy.password", "oauth.token.fingerprint"} {
		value, err := mixedStore.GetSetting(key)
		if err != nil || !secrets.IsKeyringRef(value) {
			t.Fatalf("promoted setting %q = %q, err = %v", key, value, err)
		}
	}
	mixedVault.Lock()
	proxyStored, _ := mixedStore.GetSetting("proxy.password")
	proxyValue, err := mixedVault.Resolve(proxyStored)
	if err != nil || proxyValue != "proxy-file-secret" {
		t.Fatalf("proxy setting after file lock = %q, err = %v", proxyValue, err)
	}
	oauthStored, _ := mixedStore.GetSetting("oauth.token.fingerprint")
	oauthValue, err := mixedVault.Resolve(oauthStored)
	if err != nil || oauthValue != `{"accessToken":"oauth-file-secret"}` {
		t.Fatalf("OAuth setting after file lock = %q, err = %v", oauthValue, err)
	}
}

func TestKeyringReferenceIsPreservedWhileKeyringIsTemporarilyUnavailable(t *testing.T) {
	dir := t.TempDir()
	adapter := &memoryKeyring{}
	keyringVault := secrets.NewWithKeyring(dir, adapter)
	legacyRef, err := keyringVault.Put("legacy/node/password", "legacy-keyring-password")
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenWithVault(dir, keyringVault)
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := store.EnsureDefaultWorkspace()
	authRaw, _ := json.Marshal(model.Auth{Type: "basic", Params: map[string]string{"password": legacyRef}})
	if _, err := store.db.Exec(`INSERT INTO node
		(id, workspace_id, kind, name, sort_order, auth, created_at, updated_at)
		VALUES ('temporarily-unavailable-keyring', ?, 'collection', 'legacy keyring', 0, ?, 1, 1)`, workspace.Id, string(authRaw)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("DELETE FROM setting WHERE key = ?", secretMigrationKey); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	adapter.unavailable = true
	fallbackVault := secrets.NewWithKeyring(dir, adapter)
	if err := fallbackVault.Unlock("temporary-keyring-outage"); err != nil {
		t.Fatal(err)
	}
	fallbackStore, err := OpenWithVault(dir, fallbackVault)
	if err != nil {
		t.Fatal(err)
	}
	defer fallbackStore.Close()
	var storedAuth string
	if err := fallbackStore.db.QueryRow("SELECT auth FROM node WHERE id = 'temporarily-unavailable-keyring'").Scan(&storedAuth); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(storedAuth, legacyRef) || strings.Contains(storedAuth, "secret://file/") {
		t.Fatalf("temporarily unavailable keyring reference was rewritten: %s", storedAuth)
	}
}

func TestCredentialUpdateFallsBackToFileDuringKeyringOutage(t *testing.T) {
	dir := t.TempDir()
	adapter := &memoryKeyring{}
	vault := secrets.NewWithKeyring(dir, adapter)
	if err := vault.Unlock("runtime-keyring-fallback"); err != nil {
		t.Fatal(err)
	}
	store, err := OpenWithVault(dir, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	workspace, _ := store.EnsureDefaultWorkspace()
	node, err := store.UpsertNode(model.Node{
		WorkspaceId: workspace.Id,
		Kind:        "collection",
		Name:        "fallback",
		Auth:        &model.Auth{Type: "basic", Params: map[string]string{"password": "old-password"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	adapter.unavailable = true
	node.Auth.Params["password"] = "new-password"
	if _, err := store.UpsertNode(node); err != nil {
		t.Fatalf("update during keyring outage: %v", err)
	}
	var storedAuth string
	if err := store.db.QueryRow("SELECT auth FROM node WHERE id = ?", node.Id).Scan(&storedAuth); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(storedAuth, "secret://file/") || strings.Contains(storedAuth, "secret://keyring/") {
		t.Fatalf("fallback auth = %s", storedAuth)
	}
	got, err := store.GetNode(workspace.Id, node.Id)
	if err != nil || got.Auth == nil || got.Auth.Params["password"] != "new-password" {
		t.Fatalf("fallback credential = %+v, err = %v", got.Auth, err)
	}

	adapter.unavailable = false
	if err := store.MigrateSecrets(); err != nil {
		t.Fatal(err)
	}
	got, err = store.GetNode(workspace.Id, node.Id)
	if err != nil || got.Auth == nil || got.Auth.Params["password"] != "new-password" {
		t.Fatalf("promoted credential = %+v, err = %v", got.Auth, err)
	}
}

func TestCompletedMarkersStillNormalizeCanonicalKeyringLiteral(t *testing.T) {
	dir := t.TempDir()
	adapter := &memoryKeyring{}
	store := openStoreWithMemoryKeyring(t, dir, adapter)
	workspace, _ := store.EnsureDefaultWorkspace()
	const literal = "secret://keyring/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	authRaw, _ := json.Marshal(model.Auth{Type: "basic", Params: map[string]string{"password": literal}})
	if _, err := store.db.Exec(`INSERT INTO node
		(id, workspace_id, kind, name, sort_order, auth, created_at, updated_at)
		VALUES ('completed-marker-keyring-literal', ?, 'collection', 'literal', 0, ?, 1, 1)`, workspace.Id, string(authRaw)); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{secretMigrationKey, headerSecretMigrationKey, requestValueMigrationKey, cookieSecretMigrationKey} {
		if err := store.SetSetting(key, "1"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.db.Exec("DELETE FROM setting WHERE key = ?", secretRefNormalizationKey); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openStoreWithMemoryKeyring(t, dir, adapter)
	var storedAuth string
	if err := reopened.db.QueryRow("SELECT auth FROM node WHERE id = 'completed-marker-keyring-literal'").Scan(&storedAuth); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(storedAuth, literal) || !strings.Contains(storedAuth, "secret://keyring/") {
		t.Fatalf("completed-marker literal was not normalized: %s", storedAuth)
	}
	node, err := reopened.GetNode(workspace.Id, "completed-marker-keyring-literal")
	if err != nil || node.Auth == nil || node.Auth.Params["password"] != literal {
		t.Fatalf("normalized literal = %+v, err = %v", node.Auth, err)
	}
}

func TestCanonicalReferenceLikeLiteralWithoutBackingEntryIsRekeyed(t *testing.T) {
	dir := t.TempDir()
	adapter := &memoryKeyring{}
	store := openStoreWithMemoryKeyring(t, dir, adapter)
	workspace, _ := store.EnsureDefaultWorkspace()

	const logicalKey = "node/missing-canonical-entry/auth/password"
	literal, err := store.Vault().PutPlaintext(logicalKey, "temporary-seed")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Vault().Delete(literal); err != nil {
		t.Fatal(err)
	}
	authRaw, _ := json.Marshal(model.Auth{Type: "basic", Params: map[string]string{"password": literal}})
	if _, err := store.db.Exec(`INSERT INTO node
		(id, workspace_id, kind, name, sort_order, auth, created_at, updated_at)
		VALUES ('missing-canonical-entry', ?, 'collection', 'canonical literal', 0, ?, 1, 1)`, workspace.Id, string(authRaw)); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{secretMigrationKey, headerSecretMigrationKey, requestValueMigrationKey, cookieSecretMigrationKey} {
		if err := store.SetSetting(key, "1"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.db.Exec("DELETE FROM setting WHERE key = ?", secretRefNormalizationKey); err != nil {
		t.Fatal(err)
	}

	if err := store.MigrateSecrets(); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetNode(workspace.Id, "missing-canonical-entry")
	if err != nil || got.Auth == nil || got.Auth.Params["password"] != literal {
		t.Fatalf("canonical reference-like literal = %+v, err = %v", got.Auth, err)
	}
	if resolved, err := store.Vault().Resolve(literal); err != nil || resolved != literal {
		t.Fatalf("canonical literal backing entry = %q, err = %v", resolved, err)
	}
}

func TestV2MigrationRekeysReferenceLikeTypedCredentialsAndSyncPassword(t *testing.T) {
	dir := t.TempDir()
	adapter := &memoryKeyring{}
	store := openStoreWithMemoryKeyring(t, dir, adapter)
	workspace, _ := store.EnsureDefaultWorkspace()
	const literal = "secret://file/literal-legacy-credential"
	authRaw, _ := json.Marshal(model.Auth{Type: "basic", Params: map[string]string{"password": literal}})
	if _, err := store.db.Exec(`INSERT INTO node
		(id, workspace_id, kind, name, sort_order, auth, created_at, updated_at)
		VALUES ('legacy-literal-auth', ?, 'collection', 'legacy literal', 0, ?, 1, 1)`, workspace.Id, string(authRaw)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting("sync.webdav", `{"url":"https://dav.example.test","password":"`+literal+`"}`); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting("secrets.migration.v1", "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("DELETE FROM setting WHERE key = ?", secretMigrationKey); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openStoreWithMemoryKeyring(t, dir, adapter)
	node, err := reopened.GetNode(workspace.Id, "legacy-literal-auth")
	if err != nil || node.Auth.Params["password"] != literal {
		t.Fatalf("migrated reference-like auth = %+v, err = %v", node.Auth, err)
	}
	raw, err := reopened.GetSetting("sync.webdav")
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatal(err)
	}
	passwordRef, _ := cfg["password"].(string)
	password, err := reopened.Vault().Resolve(passwordRef)
	if err != nil || password != literal {
		t.Fatalf("migrated reference-like sync password = %q, err = %v", password, err)
	}
}

func TestStructuredAuditMigrationRunsAfterLegacyV1Markers(t *testing.T) {
	dir := t.TempDir()
	adapter := &memoryKeyring{}
	store := openStoreWithMemoryKeyring(t, dir, adapter)
	workspace, _ := store.EnsureDefaultWorkspace()
	node, err := store.UpsertNode(model.Node{
		WorkspaceId: workspace.Id, Kind: "request", Name: "audit-owner",
		Request: &model.HttpRequest{Method: "GET", Url: "https://example.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := model.HttpRequest{
		Params: []model.KV{{Key: "access_token", Value: "legacy-audit-query", Enabled: true}},
		Body: model.Body{Kind: "urlencoded", Items: []model.FormItem{{
			Key: "password", Type: "text", Value: "legacy-audit-form", Enabled: true,
		}}},
	}
	raw, _ := json.Marshal(request)
	if _, err := store.db.Exec(`INSERT INTO history
		(id, workspace_id, request_snap, method, url, body_inline, created_at)
		VALUES ('legacy-v1-history', ?, ?, 'POST', '', 'legacy-audit-query legacy-audit-form', 1)`,
		workspace.Id, string(raw)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`INSERT INTO example
		(id, node_id, name, request_snap, status, headers, body, created_at, updated_at)
		VALUES ('legacy-v1-example', ?, 'legacy', ?, 200, '[]', 'legacy-audit-query legacy-audit-form', 1, 1)`,
		node.Id, string(raw)); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting("history.request-redaction.v1", "1"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting("example.redaction.v1", "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("DELETE FROM setting WHERE key IN (?, ?)", historyRequestRedactionMigrationKey, exampleRedactionMigrationKey); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openStoreWithMemoryKeyring(t, dir, adapter)
	var historyRequest, historyBody, exampleRequest, exampleBody string
	if err := reopened.db.QueryRow(
		"SELECT request_snap, body_inline FROM history WHERE id = 'legacy-v1-history'",
	).Scan(&historyRequest, &historyBody); err != nil {
		t.Fatal(err)
	}
	if err := reopened.db.QueryRow(
		"SELECT request_snap, body FROM example WHERE id = 'legacy-v1-example'",
	).Scan(&exampleRequest, &exampleBody); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(historyRequest+historyBody+exampleRequest+exampleBody, "legacy-audit-query") ||
		strings.Contains(historyRequest+historyBody+exampleRequest+exampleBody, "legacy-audit-form") {
		t.Fatalf("legacy v1 audit markers skipped structured redaction: history=%s %s example=%s %s",
			historyRequest, historyBody, exampleRequest, exampleBody)
	}
}

func TestAuditMigrationWaitsForVaultUnlockBeforeRedactingReferencedEchoes(t *testing.T) {
	dir := t.TempDir()
	adapter := &memoryKeyring{unavailable: true}
	vault := secrets.NewWithKeyring(dir, adapter)
	if err := vault.Unlock("audit-migration-test"); err != nil {
		t.Fatal(err)
	}
	store, err := OpenWithVault(dir, vault)
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := store.EnsureDefaultWorkspace()
	const secret = "referenced-history-echo"
	ref, err := vault.Put("test/history-reference", secret)
	if err != nil {
		t.Fatal(err)
	}
	request := model.HttpRequest{Params: []model.KV{{Key: "access_token", Value: ref, Enabled: true}}}
	raw, _ := json.Marshal(request)
	if _, err := store.db.Exec(`INSERT INTO history
		(id, workspace_id, request_snap, method, url, body_inline, created_at)
		VALUES ('locked-reference-history', ?, ?, 'GET', '', ?, 1)`, workspace.Id, string(raw), secret); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("DELETE FROM setting WHERE key = ?", historyRequestRedactionMigrationKey); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	lockedVault := secrets.NewWithKeyring(dir, adapter)
	lockedStore, err := OpenWithVault(dir, lockedVault)
	if err != nil {
		t.Fatal(err)
	}
	defer lockedStore.Close()
	if marker, err := lockedStore.GetSetting(historyRequestRedactionMigrationKey); err != nil || marker != "" {
		t.Fatalf("locked audit migration marker = %q, err = %v", marker, err)
	}
	if err := lockedVault.Unlock("audit-migration-test"); err != nil {
		t.Fatal(err)
	}
	if err := lockedStore.MigrateSecrets(); err != nil {
		t.Fatal(err)
	}
	if err := lockedStore.MigrateAuditSecrets(); err != nil {
		t.Fatal(err)
	}
	var storedRequest, storedBody string
	if err := lockedStore.db.QueryRow(
		"SELECT request_snap, body_inline FROM history WHERE id = 'locked-reference-history'",
	).Scan(&storedRequest, &storedBody); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(storedRequest+storedBody, secret) || storedBody != "<redacted>" {
		t.Fatalf("referenced history echo was not redacted after unlock: request=%s body=%s", storedRequest, storedBody)
	}
}

func TestLegacyHistoryRequestHeadersAreRedactedOnReopen(t *testing.T) {
	dir := t.TempDir()
	adapter := &memoryKeyring{}
	store := openStoreWithMemoryKeyring(t, dir, adapter)
	workspace, _ := store.EnsureDefaultWorkspace()
	request := model.HttpRequest{Headers: []model.KV{{Key: "Cookie", Value: "session=legacy-request-cookie", Enabled: true}}}
	raw, _ := json.Marshal(request)
	responseMeta := `{"headers":[{"key":"X-Api-Token","value":"legacy-response-token","enabled":true}],"timing":{}}`
	if _, err := store.db.Exec(`INSERT INTO history
		(id, workspace_id, request_snap, method, url, response_meta, body_inline, created_at)
		VALUES ('legacy-request-history', ?, ?, 'GET', '', ?, 'echo=legacy-request-cookie response=legacy-response-token', 1)`,
		workspace.Id, string(raw), responseMeta); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("DELETE FROM setting WHERE key = ?", historyRequestRedactionMigrationKey); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("DELETE FROM setting WHERE key = ?", historyResponseRedactionMigrationKey); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openStoreWithMemoryKeyring(t, dir, adapter)
	var stored, body string
	if err := reopened.db.QueryRow(
		"SELECT request_snap, body_inline FROM history WHERE id = 'legacy-request-history'",
	).Scan(&stored, &body); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored+body, "legacy-request-cookie") || strings.Contains(stored+body, "legacy-response-token") {
		t.Fatalf("legacy history leaked header values: request=%s body=%s", stored, body)
	}
}

func TestHistoryRedactsResponseCredentialsBeforePersistence(t *testing.T) {
	store := openStoreWithMemoryKeyring(t, t.TempDir(), &memoryKeyring{})
	workspace, _ := store.EnsureDefaultWorkspace()
	const knownSecret = "known-response-secret"
	if _, err := store.Vault().Put("test/history-response", knownSecret); err != nil {
		t.Fatal(err)
	}
	id, err := store.InsertHistory(model.HistoryRecord{
		WorkspaceId: workspace.Id,
		RequestSnap: model.HttpRequest{Method: "POST", Url: "https://example.test/login"},
		RespHeaders: []model.KV{
			{Key: "set-cookie", Value: "session=unseen-cookie-secret; HttpOnly", Enabled: true},
			{Key: "X-Debug", Value: "token=" + knownSecret, Enabled: true},
			{Key: "X-Api-Token", Value: "response-header-secret", Enabled: true},
		},
		BodyInline: `{"debug":"` + knownSecret + `","cookie":"unseen-cookie-secret","response":"response-header-secret"}`,
		TestResults: []model.TestResult{{
			Name:  "response excludes " + knownSecret,
			Pass:  false,
			Error: "received " + knownSecret + " unseen-cookie-secret response-header-secret",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var meta, body, tests string
	if err := store.db.QueryRow(
		"SELECT response_meta, body_inline, test_results FROM history WHERE id = ?", id,
	).Scan(&meta, &body, &tests); err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"unseen-cookie-secret", knownSecret, "response-header-secret"} {
		if strings.Contains(meta+body+tests, secret) {
			t.Fatalf("history response persisted %q: meta=%s body=%s tests=%s", secret, meta, body, tests)
		}
	}
	detail, err := store.GetHistory(workspace.Id, id)
	if err != nil {
		t.Fatal(err)
	}
	if got := detail.RespHeaders[0].Value; got != "<redacted>" {
		t.Fatalf("Set-Cookie value = %q", got)
	}
	if got := detail.RespHeaders[2].Value; got != "<redacted>" {
		t.Fatalf("sensitive response header value = %q", got)
	}
	if !strings.Contains(detail.RespHeaders[1].Value, "<redacted>") ||
		!strings.Contains(detail.BodyInline, "<redacted>") ||
		!strings.Contains(detail.TestResults[0].Error, "<redacted>") {
		t.Fatalf("response redaction incomplete: %+v", detail)
	}
}

func TestLegacyHistorySetCookieIsRedactedOnReopen(t *testing.T) {
	dir := t.TempDir()
	adapter := &memoryKeyring{}
	store, err := OpenWithVault(dir, secrets.NewWithKeyring(dir, adapter))
	if err != nil {
		t.Fatal(err)
	}
	workspace, _ := store.EnsureDefaultWorkspace()
	const legacySecret = "legacy-history-cookie"
	meta := `{"headers":[{"key":"Set-Cookie","value":"session=` + legacySecret + `","enabled":true}],"timing":{}}`
	if _, err := store.db.Exec(`
		INSERT INTO history (id, workspace_id, request_snap, response_meta, created_at)
		VALUES ('legacy-history', ?, '{}', ?, 1)`, workspace.Id, meta); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("DELETE FROM setting WHERE key = ?", historyResponseRedactionMigrationKey); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenWithVault(dir, secrets.NewWithKeyring(dir, adapter))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var raw string
	if err := reopened.db.QueryRow("SELECT response_meta FROM history WHERE id = 'legacy-history'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var migrated responseMeta
	if err := json.Unmarshal([]byte(raw), &migrated); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, legacySecret) || len(migrated.Headers) != 1 || migrated.Headers[0].Value != "<redacted>" {
		t.Fatalf("legacy response metadata was not redacted: %s", raw)
	}
}

func TestExamplesRedactRequestAndResponseCredentials(t *testing.T) {
	store := openStoreWithMemoryKeyring(t, t.TempDir(), &memoryKeyring{})
	workspace, _ := store.EnsureDefaultWorkspace()
	node, err := store.UpsertNode(model.Node{
		WorkspaceId: workspace.Id, Kind: "request", Name: "example-owner",
		Request: &model.HttpRequest{Method: "GET", Url: "https://example.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := model.HttpRequest{
		Headers: []model.KV{{Key: "Authorization", Value: "Bearer example-secret", Enabled: true}},
		Auth:    model.Auth{Type: "bearer", Params: map[string]string{"token": "auth-example-secret"}},
	}
	example, err := store.UpsertExample(model.Example{
		NodeId: node.Id, Name: "secured", RequestSnap: &request, Status: 200,
		Headers: []model.KV{
			{Key: "Set-Cookie", Value: "session=example-cookie", Enabled: true},
			{Key: "X-Api-Token", Value: "example-response-token", Enabled: true},
		},
		Body: "echo=example-secret cookie=example-cookie response=example-response-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	var snap, headers, body string
	if err := store.db.QueryRow(
		"SELECT request_snap, headers, body FROM example WHERE id = ?", example.Id,
	).Scan(&snap, &headers, &body); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(snap+headers+body, "example-secret") || strings.Contains(snap+headers+body, "example-cookie") ||
		strings.Contains(snap+headers+body, "example-response-token") {
		t.Fatalf("example persisted credentials: snap=%s headers=%s body=%s", snap, headers, body)
	}
}

func TestLegacyExampleCredentialsAreRedactedOnReopen(t *testing.T) {
	dir := t.TempDir()
	adapter := &memoryKeyring{}
	store := openStoreWithMemoryKeyring(t, dir, adapter)
	workspace, _ := store.EnsureDefaultWorkspace()
	node, err := store.UpsertNode(model.Node{
		WorkspaceId: workspace.Id, Kind: "request", Name: "legacy-owner",
		Request: &model.HttpRequest{Method: "GET", Url: "https://example.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := model.HttpRequest{Headers: []model.KV{{Key: "Cookie", Value: "session=legacy-example-cookie", Enabled: true}}}
	snap, _ := json.Marshal(request)
	if _, err := store.db.Exec(`INSERT INTO example
		(id, node_id, name, request_snap, status, headers, body, created_at, updated_at)
		VALUES ('legacy-example', ?, 'legacy', ?, 200, ?, 'echo=legacy-example-cookie', 1, 1)`,
		node.Id, string(snap), `[{"key":"Set-Cookie","value":"session=legacy-response-cookie","enabled":true}]`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec("DELETE FROM setting WHERE key = ?", exampleRedactionMigrationKey); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened := openStoreWithMemoryKeyring(t, dir, adapter)
	var requestRaw, headersRaw, body string
	if err := reopened.db.QueryRow(
		"SELECT request_snap, headers, body FROM example WHERE id = 'legacy-example'",
	).Scan(&requestRaw, &headersRaw, &body); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(requestRaw+headersRaw+body, "legacy-example-cookie") || strings.Contains(headersRaw, "legacy-response-cookie") {
		t.Fatalf("legacy example leaked credentials: request=%s headers=%s body=%s", requestRaw, headersRaw, body)
	}
}
