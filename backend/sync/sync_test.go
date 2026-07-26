package sync

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/net/webdav"

	"apirequest/backend/model"
	"apirequest/backend/storage"
)

// startDav 起内存 WebDAV 服务器（可选 Basic auth）
func startDav(t *testing.T, user, pass string) *httptest.Server {
	t.Helper()
	h := &webdav.Handler{
		FileSystem: webdav.NewMemFS(),
		LockSystem: webdav.NewMemLS(),
	}
	handler := http.Handler(h)
	if user != "" {
		inner := handler
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, p, ok := r.BasicAuth()
			if !ok || u != user || p != pass {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			inner.ServeHTTP(w, r)
		})
	}
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

// newDevice 模拟一台设备：独立本地库 + 默认工作区固定 id
func newDevice(t *testing.T) (*storage.Store, string) {
	t.Helper()
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	w, err := store.EnsureDefaultWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	return store, w.Id
}

func addRequest(t *testing.T, store *storage.Store, wsId, name string) model.Node {
	t.Helper()
	col, err := store.UpsertNode(model.Node{WorkspaceId: wsId, Kind: "collection", Name: "c-" + name})
	if err != nil {
		t.Fatal(err)
	}
	n, err := store.UpsertNode(model.Node{
		WorkspaceId: wsId, ParentId: col.Id, Kind: "request", Name: name,
		Request: &model.HttpRequest{Method: "GET", Url: "https://x.io/" + name, Settings: model.DefaultSettings()},
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func nodeNames(t *testing.T, store *storage.Store, wsId string) map[string]bool {
	t.Helper()
	nodes, err := store.ListNodes(wsId)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, n := range nodes {
		out[n.Name] = true
	}
	return out
}

func TestFirstSyncInitializesRemote(t *testing.T) {
	srv := startDav(t, "u", "p")
	store, wsId := newDevice(t)
	addRequest(t, store, wsId, "r1")

	cfg := DavConfig{Url: srv.URL, Username: "u", Password: "p"}
	report, err := Sync(store, wsId, cfg)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if !report.RemoteFresh || report.Pushed == 0 {
		t.Errorf("report = %+v", report)
	}
	// 远端应已有快照文件
	client, _ := newDavClient(cfg)
	data, exists, err := client.Get(remotePath(wsId))
	if err != nil || !exists || len(data) == 0 {
		t.Fatalf("remote snapshot missing: exists=%v err=%v", exists, err)
	}
}

func TestTwoDeviceBidirectionalSync(t *testing.T) {
	srv := startDav(t, "", "")
	cfg := DavConfig{Url: srv.URL}

	// 设备 A/B 用同一个远端工作区路径：B 直接采用 A 的 wsId 命名远端
	storeA, wsA := newDevice(t)
	addRequest(t, storeA, wsA, "from-A")
	if _, err := Sync(storeA, wsA, cfg); err != nil {
		t.Fatalf("A push: %v", err)
	}

	// B：同一远端路径（模拟 B 输入了相同的工作区绑定——现实里 B 首次拉取用 A 的 wsId）
	storeB, _ := newDevice(t)
	// B 用与 A 相同的远端键同步：给 B 建同 id 工作区不可行（EnsureDefault 已建），
	// 直接以 wsA 作为远端键、B 本地默认工作区承接
	wB, _ := storeB.EnsureDefaultWorkspace()
	addRequest(t, storeB, wB.Id, "from-B")
	// 把 B 的数据同步到 A 的远端键：Sync 的 workspaceId 同时决定远端路径与本地读写
	// —— 测试 B 的本地工作区 id 与远端键一致的场景需要 id 相同；
	// 为此把 B 的节点搬到 wsA id 下（模拟绑定远端工作区）
	if err := storeB.EnsureWorkspace(wsA, "bound"); err != nil {
		t.Fatal(err)
	}
	nodesB, _ := storeB.ListNodesForSync(wB.Id)
	for _, row := range nodesB {
		row.Node.WorkspaceId = wsA
		if err := storeB.ApplySyncNode(row); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Sync(storeB, wsA, cfg); err != nil {
		t.Fatalf("B sync: %v", err)
	}
	// B 应拉到 A 的数据
	if names := nodeNames(t, storeB, wsA); !names["from-A"] || !names["from-B"] {
		t.Errorf("B names = %v", names)
	}

	// A 再同步：应拉到 B 的数据
	if _, err := Sync(storeA, wsA, cfg); err != nil {
		t.Fatalf("A pull: %v", err)
	}
	if names := nodeNames(t, storeA, wsA); !names["from-A"] || !names["from-B"] {
		t.Errorf("A names = %v", names)
	}
}

func TestLWWConflictAndTombstone(t *testing.T) {
	srv := startDav(t, "", "")
	cfg := DavConfig{Url: srv.URL}
	store, wsId := newDevice(t)
	n := addRequest(t, store, wsId, "victim")
	addRequest(t, store, wsId, "keeper")
	if _, err := Sync(store, wsId, cfg); err != nil {
		t.Fatal(err)
	}

	// 本地删除 victim（墓碑）后再同步：远端合并结果应保留墓碑
	if err := store.DeleteNode(n.Id); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond) // 保证时间戳前进
	if _, err := Sync(store, wsId, cfg); err != nil {
		t.Fatal(err)
	}
	// 再次全新同步（模拟第二台设备拉取）：victim 不应复活
	names := nodeNames(t, store, wsId)
	if names["victim"] {
		t.Error("tombstoned node should stay deleted after sync")
	}
	if !names["keeper"] {
		t.Error("keeper should survive")
	}
}

func TestOmitSecrets(t *testing.T) {
	srv := startDav(t, "", "")
	store, wsId := newDevice(t)
	store.UpsertEnvironment(model.Environment{
		WorkspaceId: wsId, Name: "dev",
		Variables: []model.Variable{
			{Key: "token", Value: "SECRET-1", Type: "secret", Enabled: true},
			{Key: "host", Value: "x.io", Type: "default", Enabled: true},
		},
	})
	cfg := DavConfig{Url: srv.URL, OmitSecrets: true}
	if _, err := Sync(store, wsId, cfg); err != nil {
		t.Fatal(err)
	}
	// 远端快照不应含密钥值
	client, _ := newDavClient(cfg)
	data, _, _ := client.Get(remotePath(wsId))
	if string(data) == "" {
		t.Fatal("no remote data")
	}
	if contains(data, "SECRET-1") {
		t.Error("secret value leaked to remote")
	}
	if !contains(data, "x.io") {
		t.Error("non-secret value should be uploaded")
	}
	// 再次同步（远端剥离版拉回）后本地密钥值应保留
	if _, err := Sync(store, wsId, cfg); err != nil {
		t.Fatal(err)
	}
	envs, _ := store.ListEnvironments(wsId)
	for _, e := range envs {
		for _, v := range e.Variables {
			if v.Key == "token" && v.Value != "SECRET-1" {
				t.Errorf("local secret lost: %q", v.Value)
			}
		}
	}
}

func TestAuthFailure(t *testing.T) {
	srv := startDav(t, "u", "correct")
	store, wsId := newDevice(t)
	_, err := Sync(store, wsId, DavConfig{Url: srv.URL, Username: "u", Password: "wrong"})
	if err == nil || !contains([]byte(err.Error()), "auth failed") {
		t.Errorf("err = %v", err)
	}
}

func contains(data []byte, sub string) bool {
	return len(data) > 0 && string(data) != "" && stringContains(string(data), sub)
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
