package binding

import (
	"errors"
	"testing"

	"apirequest/backend/httpengine"
	"apirequest/backend/model"
	"apirequest/backend/secrets"
	"apirequest/backend/storage"
)

type recoverableSettingsKeyring struct {
	values      map[string]string
	unavailable bool
}

func (k *recoverableSettingsKeyring) Set(_, account, value string) error {
	if k.unavailable {
		return errors.New("keyring unavailable")
	}
	if k.values == nil {
		k.values = map[string]string{}
	}
	k.values[account] = value
	return nil
}

func (k *recoverableSettingsKeyring) Get(_, account string) (string, error) {
	if k.unavailable {
		return "", errors.New("keyring unavailable")
	}
	value, ok := k.values[account]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return value, nil
}

func (k *recoverableSettingsKeyring) Delete(_, account string) error {
	if k.unavailable {
		return errors.New("keyring unavailable")
	}
	delete(k.values, account)
	return nil
}

func TestLockVaultPromotesRuntimeFileFallbackBeforeLocking(t *testing.T) {
	dir := t.TempDir()
	adapter := &recoverableSettingsKeyring{}
	vault := secrets.NewWithKeyring(dir, adapter)
	if err := vault.Unlock("settings-lock-fallback"); err != nil {
		t.Fatal(err)
	}
	store, err := storage.OpenWithVault(dir, vault)
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
		t.Fatal(err)
	}
	adapter.unavailable = false

	status, err := NewSettingsApi(store, httpengine.New()).LockVault()
	if err != nil {
		t.Fatal(err)
	}
	if status.FileUnlocked || !status.KeyringAvailable {
		t.Fatalf("locked status = %+v", status)
	}
	got, err := store.GetNode(workspace.Id, node.Id)
	if err != nil || got.Auth == nil || got.Auth.Params["password"] != "new-password" {
		t.Fatalf("credential after locking fallback = %+v, err = %v", got.Auth, err)
	}
}
