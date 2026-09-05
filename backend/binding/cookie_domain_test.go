package binding

import (
	"testing"

	"apirequest/backend/model"
	"apirequest/backend/storage"
)

// 安全边界：服务器不得通过 Set-Cookie 的 Domain 属性把 cookie 写到无关域上。
// 越权条目必须被丢弃，但同一响应里其余合法 cookie 仍须落库
// （早先的实现遇到一条坏 domain 就整批报错，等于让坏服务器丢掉全部 cookie）。
func TestPersistCookiesDropsCrossDomainAndKeepsValidOnes(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cookies := []model.Cookie{
		{Name: "evil", Value: "1", Domain: "attacker.example"}, // 越权：与 host 无关
		{Name: "sibling", Value: "2", Domain: "other.test"},    // 越权：非 host 的后缀
		{Name: "host_only", Value: "3"},                        // 合法：Domain 空 → host-only
		{Name: "parent", Value: "4", Domain: "app.test"},       // 合法：host 的父域
		{Name: "exact", Value: "5", Domain: "api.app.test"},    // 合法：与 host 完全相同
	}
	if err := persistCookies(store, "https://api.app.test/v1/login", cookies); err != nil {
		t.Fatalf("persistCookies returned error, want cross-domain entries skipped: %v", err)
	}

	stored, err := store.ListCookies("")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range stored {
		got[c.Name] = c.Domain
	}

	for _, name := range []string{"evil", "sibling"} {
		if domain, ok := got[name]; ok {
			t.Errorf("cross-domain cookie %q was persisted with domain %q", name, domain)
		}
	}
	for _, name := range []string{"host_only", "parent", "exact"} {
		if _, ok := got[name]; !ok {
			t.Errorf("legitimate cookie %q was dropped (got %v)", name, got)
		}
	}
	if domain := got["host_only"]; domain != "api.app.test" {
		t.Errorf("host-only cookie domain = %q, want api.app.test", domain)
	}
}
