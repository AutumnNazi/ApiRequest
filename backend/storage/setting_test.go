package storage

import (
	"testing"

	"apirequest/backend/secrets"
)

func TestSetSettingsRollsBackAllValues(t *testing.T) {
	store := openTestStore(t)
	if err := store.SetSetting("first", "old"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`
		CREATE TRIGGER fail_second_setting
		BEFORE INSERT ON setting
		WHEN NEW.key = 'second'
		BEGIN
			SELECT RAISE(ABORT, 'forced setting failure');
		END`); err != nil {
		t.Fatal(err)
	}

	err := store.SetSettings(map[string]string{"first": "new", "second": "new"})
	if err == nil {
		t.Fatal("multi-setting write unexpectedly succeeded")
	}
	first, _ := store.GetSetting("first")
	second, _ := store.GetSetting("second")
	if first != "old" || second != "" {
		t.Fatalf("partial settings remained after rollback: first=%q second=%q", first, second)
	}
}

func TestUpdateSecretSettingsRollsBackDatabaseAndVault(t *testing.T) {
	adapter := &memoryKeyring{values: map[string]string{}}
	dir := t.TempDir()
	vault := secrets.NewWithKeyring(dir, adapter)
	store, err := OpenWithVault(dir, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.db.Exec(`
		CREATE TRIGGER fail_proxy_password_setting
		BEFORE INSERT ON setting
		WHEN NEW.key = 'proxy.password'
		BEGIN
			SELECT RAISE(ABORT, 'forced proxy setting failure');
		END`); err != nil {
		t.Fatal(err)
	}

	err = store.UpdateSecretSettings(
		[]string{"proxy.url", "proxy.password"},
		func(_ map[string]string, writer secrets.SecretWriter) (map[string]string, error) {
			ref, err := writer.Put("setting/proxy/password", "proxy-secret")
			if err != nil {
				return nil, err
			}
			return map[string]string{
				"proxy.url":      "http://proxy.example:8080",
				"proxy.password": ref,
			}, nil
		},
	)
	if err == nil {
		t.Fatal("secret settings write unexpectedly succeeded")
	}
	if len(adapter.values) != 0 {
		t.Fatalf("Vault write survived database rollback: %+v", adapter.values)
	}
	for _, key := range []string{"proxy.url", "proxy.password"} {
		value, getErr := store.GetSetting(key)
		if getErr != nil || value != "" {
			t.Fatalf("setting %q = %q, err = %v", key, value, getErr)
		}
	}
}
