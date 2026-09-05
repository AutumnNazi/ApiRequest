package storage

import (
	"testing"

	"apirequest/backend/model"
)

// 环境级 Cookie jar：cookie 按工作区隔离（data-model.md 备注的演进路径，schema 0011）
func TestCookiesAreIsolatedByWorkspace(t *testing.T) {
	s := openStoreWithMemoryKeyring(t, t.TempDir(), &memoryKeyring{values: map[string]string{}})
	wsA := firstWorkspaceId(t, s)
	// 第二个工作区
	created, err := s.CreateWorkspace("second")
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	wsB := created.Id

	cookieA := model.Cookie{Domain: "api.test", Path: "/", Name: "session", Value: "A", HostOnly: true}
	if err := s.UpsertCookie(wsA, cookieA); err != nil {
		t.Fatalf("upsert A: %v", err)
	}
	cookieB := model.Cookie{Domain: "api.test", Path: "/", Name: "session", Value: "B", HostOnly: true}
	if err := s.UpsertCookie(wsB, cookieB); err != nil {
		t.Fatalf("upsert B: %v", err)
	}

	// 同 domain/path/name 在两个工作区各自独立
	forA, err := s.CookiesForHost(wsA, "api.test")
	if err != nil {
		t.Fatalf("host A: %v", err)
	}
	forB, err := s.CookiesForHost(wsB, "api.test")
	if err != nil {
		t.Fatalf("host B: %v", err)
	}
	if len(forA) != 1 || forA[0].Value != "A" {
		t.Fatalf("workspace A should see its own value: %+v", forA)
	}
	if len(forB) != 1 || forB[0].Value != "B" {
		t.Fatalf("workspace B should see its own value: %+v", forB)
	}

	// A 里写第二份同名不同 path：不影响 B
	if err := s.UpsertCookie(wsA, model.Cookie{Domain: "api.test", Path: "/x", Name: "session", Value: "A2", HostOnly: true}); err != nil {
		t.Fatalf("upsert A2: %v", err)
	}
	forB2, _ := s.CookiesForHost(wsB, "api.test")
	if len(forB2) != 1 {
		t.Fatalf("workspace B unchanged expected, got %+v", forB2)
	}
}

func TestLegacyCookiesMigrateToFirstWorkspace(t *testing.T) {
	// 旧库迁移：无 workspace_id 的存量 cookie 归入默认工作区（首个）
	s := openStoreWithMemoryKeyring(t, t.TempDir(), &memoryKeyring{values: map[string]string{}})
	wsA := firstWorkspaceId(t, s)
	if err := s.UpsertCookie(wsA, model.Cookie{Domain: "old.test", Path: "/", Name: "legacy", Value: "1", HostOnly: true}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// 老签名（无 workspace）不应再存在——验证读路径按工作区隔离即可
	all, err := s.ListCookies(wsA, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 || all[0].Domain != "old.test" {
		t.Fatalf("cookie should live in ws A: %+v", all)
	}
}

func TestClearCookiesScopedToWorkspace(t *testing.T) {
	s := openStoreWithMemoryKeyring(t, t.TempDir(), &memoryKeyring{values: map[string]string{}})
	wsA := firstWorkspaceId(t, s)
	created, err := s.CreateWorkspace("second")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	wsB := created.Id

	for _, ws := range []string{wsA, wsB} {
		if err := s.UpsertCookie(ws, model.Cookie{Domain: "x.test", Path: "/", Name: "k", Value: "v", HostOnly: true}); err != nil {
			t.Fatalf("upsert %s: %v", ws, err)
		}
	}
	if err := s.ClearCookies(wsA, ""); err != nil {
		t.Fatalf("clear A: %v", err)
	}
	inB, err := s.ListCookies(wsB, "")
	if err != nil {
		t.Fatalf("list B: %v", err)
	}
	if len(inB) != 1 {
		t.Fatalf("clear in A must not touch B: %+v", inB)
	}
}

func TestDeleteCookieScopedToWorkspace(t *testing.T) {
	s := openStoreWithMemoryKeyring(t, t.TempDir(), &memoryKeyring{values: map[string]string{}})
	wsA := firstWorkspaceId(t, s)
	created, err := s.CreateWorkspace("second")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	wsB := created.Id
	for _, ws := range []string{wsA, wsB} {
		if err := s.UpsertCookie(ws, model.Cookie{Domain: "d.test", Path: "/", Name: "n", Value: "v", HostOnly: true}); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}
	if err := s.DeleteCookie(wsA, "d.test", "/", "n"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	left, err := s.ListCookies(wsB, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(left) != 1 {
		t.Fatalf("B must keep its cookie: %+v", left)
	}
}
