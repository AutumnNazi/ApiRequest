package binding

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"apirequest/backend/httpengine"
	"apirequest/backend/secrets"
	"apirequest/backend/storage"
	appsync "apirequest/backend/sync"
)

type bindingMemoryKeyring struct{ values map[string]string }

func (m *bindingMemoryKeyring) Set(_, account, value string) error {
	if m.values == nil {
		m.values = map[string]string{}
	}
	m.values[account] = value
	return nil
}
func (m *bindingMemoryKeyring) Get(_, account string) (string, error) {
	value, ok := m.values[account]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return value, nil
}
func (m *bindingMemoryKeyring) Delete(_, account string) error {
	if _, ok := m.values[account]; !ok {
		return errors.New("not found")
	}
	delete(m.values, account)
	return nil
}

func TestSyncConfigPasswordStaysInVaultAndOutOfPublicResponse(t *testing.T) {
	dir := t.TempDir()
	vault := secrets.NewWithKeyring(dir, &bindingMemoryKeyring{})
	store, err := storage.OpenWithVault(dir, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api := NewSyncApi(store, httpengine.New())
	if err := api.SetSyncConfig(appsync.DavConfig{
		Url: "https://dav.example.test", Username: "alice", Password: "webdav-secret", OmitSecrets: true,
	}); err != nil {
		t.Fatal(err)
	}
	raw, err := store.GetSetting("sync.webdav")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "webdav-secret") || !strings.Contains(raw, "secret://keyring/") {
		t.Fatalf("unsafe stored config: %s", raw)
	}
	public, err := api.GetSyncConfig()
	if err != nil {
		t.Fatal(err)
	}
	if public.Password != "" || !public.PasswordSet {
		t.Fatalf("public config exposes or loses password state: %+v", public)
	}
	resolved, err := api.loadSyncConfig(true)
	if err != nil || resolved.Password != "webdav-secret" {
		t.Fatalf("resolved config = %+v, err = %v", resolved, err)
	}

	if err := api.SetSyncConfig(appsync.DavConfig{Url: public.Url, Username: public.Username, OmitSecrets: public.OmitSecrets}); err != nil {
		t.Fatal(err)
	}
	resolved, err = api.loadSyncConfig(true)
	if err != nil || resolved.Password != "webdav-secret" {
		t.Fatalf("blank password did not preserve existing secret: %+v, err = %v", resolved, err)
	}
}

func TestSyncConfigUsesStableReferenceAndClearRemovesIt(t *testing.T) {
	dir := t.TempDir()
	keyring := &bindingMemoryKeyring{}
	vault := secrets.NewWithKeyring(dir, keyring)
	store, err := storage.OpenWithVault(dir, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api := NewSyncApi(store, httpengine.New())
	if err := api.SetSyncConfig(appsync.DavConfig{Url: "https://dav.example.test", Password: "old-secret"}); err != nil {
		t.Fatal(err)
	}
	firstRaw, err := store.GetSetting("sync.webdav")
	if err != nil {
		t.Fatal(err)
	}
	var first appsync.DavConfig
	if err := json.Unmarshal([]byte(firstRaw), &first); err != nil {
		t.Fatal(err)
	}
	if err := api.SetSyncConfig(appsync.DavConfig{Url: "https://dav.example.test", Password: "new-secret"}); err != nil {
		t.Fatal(err)
	}
	secondRaw, err := store.GetSetting("sync.webdav")
	if err != nil {
		t.Fatal(err)
	}
	var second appsync.DavConfig
	if err := json.Unmarshal([]byte(secondRaw), &second); err != nil {
		t.Fatal(err)
	}
	if first.Password == "" || second.Password != first.Password || len(keyring.values) != 1 {
		t.Fatalf("password reference was not stable: first=%q second=%q values=%+v", first.Password, second.Password, keyring.values)
	}
	if value, err := vault.Resolve(second.Password); err != nil || value != "new-secret" {
		t.Fatalf("updated password = %q, err = %v", value, err)
	}
	if err := api.SetSyncConfig(appsync.DavConfig{Url: "https://dav.example.test", ClearPassword: true}); err != nil {
		t.Fatal(err)
	}
	public, err := api.GetSyncConfig()
	if err != nil || public.PasswordSet || public.Password != "" || len(keyring.values) != 0 {
		t.Fatalf("clear left password state behind: %+v, values=%+v, err=%v", public, keyring.values, err)
	}
}

func TestSyncConfigPasswordUpdateIsCompensatedOnSettingFailure(t *testing.T) {
	dir := t.TempDir()
	keyring := &bindingMemoryKeyring{}
	vault := secrets.NewWithKeyring(dir, keyring)
	store, err := storage.OpenWithVault(dir, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api := NewSyncApi(store, httpengine.New())
	if err := api.SetSyncConfig(appsync.DavConfig{Url: "https://dav.example.test", Password: "old-secret"}); err != nil {
		t.Fatal(err)
	}
	raw, err := store.GetSetting("sync.webdav")
	if err != nil {
		t.Fatal(err)
	}
	var original appsync.DavConfig
	if err := json.Unmarshal([]byte(raw), &original); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", filepath.Join(dir, "apirequest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER fail_sync_setting
		BEFORE UPDATE ON setting WHEN NEW.key = 'sync.webdav'
		BEGIN SELECT RAISE(FAIL, 'injected setting failure'); END`); err != nil {
		t.Fatal(err)
	}

	if err := api.SetSyncConfig(appsync.DavConfig{Url: "https://changed.example.test", Password: "new-secret"}); err == nil {
		t.Fatal("injected setting failure was ignored")
	}
	if len(keyring.values) != 1 {
		t.Fatalf("failed update leaked a Vault entry: %+v", keyring.values)
	}
	resolved, err := vault.Resolve(original.Password)
	if err != nil || resolved != "old-secret" {
		t.Fatalf("old password was damaged after failed update: %q, %v", resolved, err)
	}
	if err := api.SetSyncConfig(appsync.DavConfig{Url: "https://changed.example.test", ClearPassword: true}); err == nil {
		t.Fatal("injected clear failure was ignored")
	}
	resolved, err = vault.Resolve(original.Password)
	if err != nil || resolved != "old-secret" {
		t.Fatalf("old password was deleted after failed clear: %q, %v", resolved, err)
	}
}

func TestSyncConfigTreatsReferenceLikePasswordAsPlaintext(t *testing.T) {
	dir := t.TempDir()
	vault := secrets.NewWithKeyring(dir, &bindingMemoryKeyring{})
	store, err := storage.OpenWithVault(dir, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	api := NewSyncApi(store, httpengine.New())
	const password = "secret://file/literal-password"
	if err := api.SetSyncConfig(appsync.DavConfig{Url: "https://dav.example.test", Password: password}); err != nil {
		t.Fatal(err)
	}
	resolved, err := api.loadSyncConfig(true)
	if err != nil || resolved.Password != password {
		t.Fatalf("reference-like password was misinterpreted: %+v, %v", resolved, err)
	}
}
