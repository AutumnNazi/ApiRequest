package binding

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"apirequest/backend/httpengine"
	"apirequest/backend/model"
	"apirequest/backend/secrets"
	"apirequest/backend/storage"
)

func openSettingsTestStore(t *testing.T) (*storage.Store, *recoverableSettingsKeyring) {
	t.Helper()
	adapter := &recoverableSettingsKeyring{}
	dir := t.TempDir()
	store, err := storage.OpenWithVault(dir, secrets.NewWithKeyring(dir, adapter))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, adapter
}

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

func TestProxyCredentialsAreStoredOnlyInVault(t *testing.T) {
	store, adapter := openSettingsTestStore(t)
	api := NewSettingsApi(store, httpengine.New())
	if err := api.SetProxySettings(ProxySettings{
		Mode: "manual",
		Url:  "http://proxy-user:proxy-password@proxy.example:8080",
	}); err != nil {
		t.Fatal(err)
	}

	storedURL, _ := store.GetSetting("proxy.url")
	storedUsername, _ := store.GetSetting("proxy.username")
	storedPassword, _ := store.GetSetting("proxy.password")
	if strings.Contains(storedURL, "proxy-password") || storedURL != "http://proxy.example:8080" {
		t.Fatalf("stored proxy URL = %q", storedURL)
	}
	if storedUsername != "proxy-user" || !secrets.IsRef(storedPassword) {
		t.Fatalf("stored proxy credentials = username:%q password:%q", storedUsername, storedPassword)
	}
	if len(adapter.values) != 1 {
		t.Fatalf("Vault values = %+v", adapter.values)
	}
	for _, value := range adapter.values {
		if value != "proxy-password" {
			t.Fatalf("Vault password = %q", value)
		}
	}
	public, err := api.GetProxySettings()
	if err != nil {
		t.Fatal(err)
	}
	if public.Password != "" || !public.PasswordSet || public.Username != "proxy-user" {
		t.Fatalf("public proxy settings leaked or lost credentials: %+v", public)
	}
}

func TestInvalidManualProxyDoesNotChangePersistedSettings(t *testing.T) {
	store, _ := openSettingsTestStore(t)
	api := NewSettingsApi(store, httpengine.New())
	if err := api.SetProxySettings(ProxySettings{Mode: "manual", Url: "http://proxy.example:8080"}); err != nil {
		t.Fatal(err)
	}
	if err := api.SetProxySettings(ProxySettings{Mode: "manual", Url: "://invalid"}); err == nil {
		t.Fatal("invalid manual proxy unexpectedly succeeded")
	}
	storedMode, _ := store.GetSetting("proxy.mode")
	storedURL, _ := store.GetSetting("proxy.url")
	if storedMode != "manual" || storedURL != "http://proxy.example:8080" {
		t.Fatalf("invalid proxy changed settings: mode=%q url=%q", storedMode, storedURL)
	}
	if err := store.SetSettings(map[string]string{"proxy.mode": "manual", "proxy.url": "://stale"}); err != nil {
		t.Fatal(err)
	}
	if err := api.SetProxySettings(ProxySettings{Mode: "none"}); err != nil {
		t.Fatalf("switching away from stale manual proxy failed: %v", err)
	}
	storedMode, _ = store.GetSetting("proxy.mode")
	storedURL, _ = store.GetSetting("proxy.url")
	if storedMode != "none" || storedURL != "" {
		t.Fatalf("stale proxy was not discarded: mode=%q url=%q", storedMode, storedURL)
	}
	if err := store.SetSettings(map[string]string{
		"proxy.mode":     "manual",
		"proxy.url":      "http://proxy.example:8080",
		"proxy.username": "proxy-user",
	}); err != nil {
		t.Fatal(err)
	}
	ref, err := store.Vault().Put("setting/proxy/password", "proxy-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetSetting("proxy.password", ref); err != nil {
		t.Fatal(err)
	}
	if err := api.SetProxySettings(ProxySettings{Mode: "none"}); err != nil {
		t.Fatalf("switching mode without proxy username failed: %v", err)
	}
}

func TestSetProxySettingsKeepsRuntimeConfigWhenStorageFails(t *testing.T) {
	store, _ := openSettingsTestStore(t)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "direct")
	}))
	defer target.Close()
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "proxied")
	}))
	defer proxy.Close()

	engine := httpengine.New()
	api := NewSettingsApi(store, engine)
	if err := api.SetProxySettings(ProxySettings{Mode: "manual", Url: proxy.URL}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := api.SetProxySettings(ProxySettings{Mode: "none"}); err == nil {
		t.Fatal("setting update unexpectedly succeeded after storage closed")
	}

	response, err := engine.NewHTTPClient(0).Get(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if string(body) != "proxied" {
		t.Fatalf("runtime proxy changed after failed persistence: %q", body)
	}
}

func TestSettingsStartupReportsInvalidNetworkConfiguration(t *testing.T) {
	store, _ := openSettingsTestStore(t)
	if err := store.SetSettings(map[string]string{
		"proxy.mode":     "manual",
		"proxy.url":      "://invalid",
		"tls.caCertPath": "missing-ca.pem",
	}); err != nil {
		t.Fatal(err)
	}
	api := NewSettingsApi(store, httpengine.New())
	status := api.GetNetworkStatus()
	if status.ProxyWarning == "" || status.TLSWarning == "" {
		t.Fatalf("startup diagnostics = %+v", status)
	}
}
