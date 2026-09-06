package sync

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ── 发现：PROPFIND 列出远端工作区快照 ──

func startRemoteWsDav(t *testing.T, snapshots map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "PROPFIND":
			if !strings.Contains(r.URL.Path, "ApiRequest") {
				http.NotFound(w, r)
				return
			}
			var body strings.Builder
			body.WriteString(`<?xml version="1.0"?><D:multistatus xmlns:D="DAV:">`)
			body.WriteString(`<D:response><D:href>/ApiRequest/</D:href></D:response>`)
			for id := range snapshots {
				body.WriteString(fmt.Sprintf(
					`<D:response><D:href>/ApiRequest/workspace-%s.json</D:href></D:response>`, id))
			}
			body.WriteString(`</D:multistatus>`)
			io_WriteString(w, body.String())
		case http.MethodGet:
			p := r.URL.Path
			if !strings.HasSuffix(p, ".json") {
				http.NotFound(w, r)
				return
			}
			id := strings.TrimSuffix(strings.TrimPrefix(p, "/ApiRequest/workspace-"), ".json")
			if snap, ok := snapshots[id]; ok {
				io_WriteString(w, snap)
				return
			}
			http.NotFound(w, r)
		case http.MethodPut:
			w.WriteHeader(http.StatusCreated)
		case "MKCOL":
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func io_WriteString(w http.ResponseWriter, s string) {
	_, _ = w.Write([]byte(s))
}

func TestDiscoverRemoteWorkspaces(t *testing.T) {
	snap := `{"schemaVersion":2,"workspaceName":"团队集合","syncedAt":1234,"nodes":[],"environments":[],"globals":[],"globalsRev":0}`
	srv := startRemoteWsDav(t, map[string]string{"ws-abc": snap})
	infos, err := DiscoverRemoteWorkspaces(DavConfig{Url: srv.URL})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("infos = %+v", infos)
	}
	info := infos[0]
	if info.WorkspaceId != "ws-abc" || info.Name != "团队集合" || info.SyncedAt != 1234 {
		t.Fatalf("info = %+v", info)
	}
}

// 单个快照损坏/缺失不影响其余条目
func TestDiscoverRemoteWorkspacesSkipsCorrupt(t *testing.T) {
	srv := startRemoteWsDav(t, map[string]string{
		"ws-good": `{"schemaVersion":2,"workspaceName":"ok","syncedAt":7,"nodes":[],"environments":[],"globals":[],"globalsRev":0}`,
		"ws-bad":  `not json`,
	})
	infos, err := DiscoverRemoteWorkspaces(DavConfig{Url: srv.URL})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(infos) != 1 || infos[0].WorkspaceId != "ws-good" {
		t.Fatalf("infos = %+v", infos)
	}
}

// ── 导入：按远端快照建立本地工作区（同 id 绑定后续同步）──

func TestImportRemoteWorkspace(t *testing.T) {
	snap := `{"schemaVersion":2,"workspaceName":"导入的工作区","syncedAt":42,"nodes":[` +
		`{"id":"col-1","kind":"collection","name":"c","createdAt":1,"updatedAt":1}` +
		`],"environments":[],"globals":[],"globalsRev":0}`
	srv := startRemoteWsDav(t, map[string]string{"ws-import-1": snap})
	cfg := DavConfig{Url: srv.URL}

	// 发现 → 导入
	infos, err := DiscoverRemoteWorkspaces(cfg)
	if err != nil || len(infos) != 1 {
		t.Fatalf("discover: %v %+v", err, infos)
	}
	store, _ := newDevice(t)
	rep, err := ImportWorkspace(store, cfg, "ws-import-1")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if rep.Pulled != 1 {
		t.Fatalf("report = %+v, want pulled=1", rep)
	}
	// 本地建了同 id 工作区且实体就位
	if _, err := store.GetNode("ws-import-1", "col-1"); err != nil {
		t.Fatalf("imported node missing: %v", err)
	}
	// 基线已写：下轮同步为三路合并（且应无变更）
	if _, _, ok, _ := store.GetSyncBase("ws-import-1"); !ok {
		t.Fatal("baseline missing after import")
	}
	next, err := Sync(store, "ws-import-1", cfg)
	if err != nil {
		t.Fatalf("post-import sync: %v", err)
	}
	if next.Pushed != 0 || next.Pulled != 0 {
		t.Fatalf("post-import sync = %+v, want no-op", next)
	}

	// 重复导入报错
	if _, err := ImportWorkspace(store, cfg, "ws-import-1"); err == nil {
		t.Fatal("re-import must fail")
	}
}

// 远端不存在该 id → 明确错误
func TestImportRemoteWorkspaceMissing(t *testing.T) {
	srv := startRemoteWsDav(t, map[string]string{})
	store, _ := newDevice(t)
	if _, err := ImportWorkspace(store, DavConfig{Url: srv.URL}, "nope"); err == nil {
		t.Fatal("expected error for missing remote workspace")
	}
}
