package storage

import (
	"sync"
	"testing"

	"apirequest/backend/model"
	"apirequest/backend/secrets"
)

func openVariableMutationTestStore(t *testing.T) *Store {
	t.Helper()
	dataDir := t.TempDir()
	vault := secrets.NewWithKeyring(dataDir, nil)
	if err := vault.Unlock("variable-mutation-test"); err != nil {
		t.Fatalf("unlock test vault: %v", err)
	}
	store, err := OpenWithVault(dataDir, vault)
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestApplyWorkspaceVariableMutationsMergesConcurrentChanges(t *testing.T) {
	store := openVariableMutationTestStore(t)
	workspace, err := store.EnsureDefaultWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	environment, err := store.UpsertEnvironment(model.Environment{
		WorkspaceId: workspace.Id,
		Name:        "shared",
		Variables:   []model.Variable{},
	})
	if err != nil {
		t.Fatal(err)
	}

	mutations := []VariableMutation{
		{Set: map[string]string{"alpha": "one"}, Unset: map[string]bool{}},
		{Set: map[string]string{"beta": "two"}, Unset: map[string]bool{}},
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(mutations))
	for _, mutation := range mutations {
		wg.Add(1)
		go func(change VariableMutation) {
			defer wg.Done()
			errs <- store.ApplyWorkspaceVariableMutations(workspace.Id, WorkspaceVariableMutations{
				EnvironmentId: environment.Id,
				Environment:   change,
			})
		}(mutation)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	updated, err := store.GetEnvironment(environment.Id)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, variable := range updated.Variables {
		values[variable.Key] = variable.Value
	}
	if values["alpha"] != "one" || values["beta"] != "two" {
		t.Fatalf("concurrent changes were lost: %+v", values)
	}
}

func TestApplyWorkspaceVariableMutationsRollsBackAllScopes(t *testing.T) {
	store := openVariableMutationTestStore(t)
	workspace, err := store.EnsureDefaultWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	environment, err := store.UpsertEnvironment(model.Environment{
		WorkspaceId: workspace.Id,
		Name:        "shared",
		Variables:   []model.Variable{{Key: "env", Value: "before", Type: "secret", Enabled: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetGlobalVariables(workspace.Id, []model.Variable{{Key: "global", Value: "before", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`CREATE TRIGGER fail_script_global_update
		BEFORE UPDATE ON global_var BEGIN SELECT RAISE(FAIL, 'injected global failure'); END`); err != nil {
		t.Fatal(err)
	}

	err = store.ApplyWorkspaceVariableMutations(workspace.Id, WorkspaceVariableMutations{
		EnvironmentId: environment.Id,
		Environment: VariableMutation{
			Set: map[string]string{"env": "after"}, Unset: map[string]bool{},
		},
		Globals: VariableMutation{
			Set: map[string]string{"global": "after"}, Unset: map[string]bool{},
		},
	})
	if err == nil {
		t.Fatal("injected global write failure was ignored")
	}

	updatedEnvironment, err := store.GetEnvironment(environment.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got := updatedEnvironment.Variables[0].Value; got != "before" {
		t.Fatalf("environment mutation was not rolled back: %q", got)
	}
	globals, err := store.GetGlobalVariables(workspace.Id)
	if err != nil {
		t.Fatal(err)
	}
	if got := globals[0].Value; got != "before" {
		t.Fatalf("global mutation changed after rollback: %q", got)
	}
}
