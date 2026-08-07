package storage

import (
	"strings"
	"testing"

	"apirequest/backend/model"
	"apirequest/backend/secrets"
)

func TestCookieValuesAreStoredInVaultAndResolvedAtInterface(t *testing.T) {
	adapter := &memoryKeyring{}
	store := openStoreWithMemoryKeyring(t, t.TempDir(), adapter)
	cookie := model.Cookie{
		Name: "session", Value: "session-secret", Domain: "example.test", Path: "/", HttpOnly: true,
	}
	if err := store.UpsertCookie(cookie); err != nil {
		t.Fatal(err)
	}

	var raw string
	if err := store.db.QueryRow("SELECT value FROM cookie WHERE name = 'session'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, cookie.Value) || !secrets.IsRef(raw) {
		t.Fatalf("cookie value stored unsafely: %q", raw)
	}
	listed, err := store.ListCookies("example.test")
	if err != nil || len(listed) != 1 || listed[0].Value != cookie.Value {
		t.Fatalf("cookies = %+v, err = %v", listed, err)
	}

	cookie.Value = "rotated-secret"
	if err := store.UpsertCookie(cookie); err != nil {
		t.Fatal(err)
	}
	if len(adapter.values) != 1 {
		t.Fatalf("rotation left %d Vault entries, want 1", len(adapter.values))
	}
	if err := store.DeleteCookie(cookie.Domain, cookie.Path, cookie.Name); err != nil {
		t.Fatal(err)
	}
	if len(adapter.values) != 0 {
		t.Fatalf("delete left Vault entries: %+v", adapter.values)
	}
}

func TestUpsertCookiesRollsBackVaultAndDatabaseAsOneBatch(t *testing.T) {
	adapter := &memoryKeyring{}
	store := openStoreWithMemoryKeyring(t, t.TempDir(), adapter)
	if _, err := store.db.Exec(`
		CREATE TRIGGER fail_second_cookie
		BEFORE INSERT ON cookie WHEN NEW.name = 'second'
		BEGIN SELECT RAISE(ABORT, 'forced cookie failure'); END`); err != nil {
		t.Fatal(err)
	}

	err := store.UpsertCookies([]model.Cookie{
		{Name: "first", Value: "first-secret", Domain: "example.test", Path: "/"},
		{Name: "second", Value: "second-secret", Domain: "example.test", Path: "/"},
	})
	if err == nil {
		t.Fatal("forced batch failure was ignored")
	}
	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM cookie").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 || len(adapter.values) != 0 {
		t.Fatalf("partial cookie batch: rows=%d Vault=%+v", count, adapter.values)
	}
}

func TestLegacyPlaintextCookiesMigrateOnReopen(t *testing.T) {
	dir := t.TempDir()
	locked, err := OpenWithVault(dir, secrets.NewWithKeyring(dir, &memoryKeyring{unavailable: true}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := locked.db.Exec(`
		INSERT INTO cookie (id, domain, path, name, value, http_only, secure)
		VALUES ('legacy-cookie', 'example.test', '/', 'session', 'legacy-cookie-secret', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if err := locked.Close(); err != nil {
		t.Fatal(err)
	}

	migrated := openStoreWithMemoryKeyring(t, dir, &memoryKeyring{})
	var raw string
	if err := migrated.db.QueryRow("SELECT value FROM cookie WHERE id = 'legacy-cookie'").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "legacy-cookie-secret") || !secrets.IsRef(raw) {
		t.Fatalf("legacy cookie was not migrated: %q", raw)
	}
	listed, err := migrated.ListCookies("example.test")
	if err != nil || len(listed) != 1 || listed[0].Value != "legacy-cookie-secret" {
		t.Fatalf("migrated cookies = %+v, err = %v", listed, err)
	}
}

func TestCookiesForHostEnforcesScopeAndLongestPathOrder(t *testing.T) {
	store := openStoreWithMemoryKeyring(t, t.TempDir(), &memoryKeyring{})
	for _, cookie := range []model.Cookie{
		{Name: "host", Value: "host-only", Domain: "example.test", Path: "/", HostOnly: true},
		{Name: "domain", Value: "domain-wide", Domain: ".example.test", Path: "/"},
		{Name: "specific", Value: "specific-path", Domain: "example.test", Path: "/api", HostOnly: true},
	} {
		if err := store.UpsertCookie(cookie); err != nil {
			t.Fatal(err)
		}
	}

	root, err := store.CookiesForHost("example.test")
	if err != nil || len(root) != 3 {
		t.Fatalf("root cookies = %+v, err = %v", root, err)
	}
	if root[0].Name != "specific" {
		t.Fatalf("cookie order = %+v, want longest path first", root)
	}
	subdomain, err := store.CookiesForHost("sub.example.test")
	if err != nil || len(subdomain) != 1 || subdomain[0].Name != "domain" {
		t.Fatalf("subdomain cookies = %+v, err = %v", subdomain, err)
	}
	if err := store.UpsertCookie(model.Cookie{
		Name: "unsafe", Value: "value", Domain: "com", Path: "/",
	}); err == nil {
		t.Fatal("public-suffix cookie domain was accepted")
	}
}
