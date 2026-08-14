package binding

import (
	"testing"

	"apirequest/backend/model"
	"apirequest/backend/script"
	"apirequest/backend/secrets"
	"apirequest/backend/storage"
)

func TestPersistVariableChangesMergesLatestEnvironmentState(t *testing.T) {
	dataDir := t.TempDir()
	vault := secrets.NewWithKeyring(dataDir, nil)
	if err := vault.Unlock("request-lifecycle-test"); err != nil {
		t.Fatal(err)
	}
	store, err := storage.OpenWithVault(dataDir, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	workspace, err := store.EnsureDefaultWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	environment, err := store.UpsertEnvironment(model.Environment{
		WorkspaceId: workspace.Id,
		Name:        "shared",
		Variables: []model.Variable{
			{Key: "token", Value: "old", Enabled: true},
			{Key: "region", Value: "initial", Enabled: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	stale := environment
	environment.Variables[1].Value = "changed-by-another-request"
	if _, err := store.UpsertEnvironment(environment); err != nil {
		t.Fatal(err)
	}

	emptyChanges := func() *script.VarChanges {
		return &script.VarChanges{Set: map[string]string{}, Unset: map[string]bool{}}
	}
	result := script.Result{
		EnvChanges: &script.VarChanges{
			Set:   map[string]string{"token": "rotated"},
			Unset: map[string]bool{},
		},
		CollectionChanges: emptyChanges(),
		GlobalChanges:     emptyChanges(),
	}
	if err := persistVariableChanges(store, &executionContext{env: &stale}, workspace.Id, result); err != nil {
		t.Fatal(err)
	}

	updated, err := store.GetEnvironment(environment.Id)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, variable := range updated.Variables {
		values[variable.Key] = variable.Value
	}
	if values["token"] != "rotated" || values["region"] != "changed-by-another-request" {
		t.Fatalf("variable changes overwrote current state: %+v", values)
	}
}

func TestPersistVariableChangesSkipsUnavailableScopes(t *testing.T) {
	dataDir := t.TempDir()
	vault := secrets.NewWithKeyring(dataDir, nil)
	if err := vault.Unlock("request-lifecycle-test"); err != nil {
		t.Fatal(err)
	}
	store, err := storage.OpenWithVault(dataDir, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	workspace, err := store.EnsureDefaultWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	result := script.Result{
		EnvChanges: &script.VarChanges{
			Set: map[string]string{"environment-only": "ignored"}, Unset: map[string]bool{},
		},
		CollectionChanges: &script.VarChanges{
			Set: map[string]string{"collection-only": "ignored"}, Unset: map[string]bool{},
		},
		GlobalChanges: &script.VarChanges{
			Set: map[string]string{"global": "persisted"}, Unset: map[string]bool{},
		},
	}
	if err := persistVariableChanges(store, &executionContext{}, workspace.Id, result); err != nil {
		t.Fatalf("unavailable scopes should be skipped: %v", err)
	}

	globals, err := store.GetGlobalVariables(workspace.Id)
	if err != nil {
		t.Fatal(err)
	}
	if len(globals) != 1 || globals[0].Key != "global" || globals[0].Value != "persisted" {
		t.Fatalf("global changes were not persisted: %+v", globals)
	}
}
