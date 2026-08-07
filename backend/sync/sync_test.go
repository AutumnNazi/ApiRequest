package sync

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/net/webdav"

	"apirequest/backend/model"
	"apirequest/backend/secrets"
	"apirequest/backend/storage"
)

type syncRoundTripper func(*http.Request) (*http.Response, error)

func (fn syncRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

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
	dataDir := t.TempDir()
	vault := secrets.NewWithKeyring(dataDir, nil)
	if err := vault.Unlock("sync-test-device"); err != nil {
		t.Fatal(err)
	}
	store, err := storage.OpenWithVault(dataDir, vault)
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

func TestBuildLocalSnapshotRejectsMissingWorkspace(t *testing.T) {
	store, _ := newDevice(t)
	if _, err := buildLocalSnapshot(store, "missing-workspace"); err == nil {
		t.Fatal("missing workspace was accepted")
	}
}

func TestDavGetRejectsSnapshotAboveLimitFromContentLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(maxSnapshotSize+1))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	client, err := newDavClient(DavConfig{Url: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := client.Get("oversized.json"); err == nil {
		t.Fatal("oversized WebDAV snapshot was accepted")
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
	// B 绑定远端工作区 id 后，直接在该 Workspace 中创建本地数据。
	if err := storeB.EnsureWorkspace(wsA, "bound"); err != nil {
		t.Fatal(err)
	}
	addRequest(t, storeB, wsA, "from-B")
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
	time.Sleep(20 * time.Millisecond) // 确保 deletedAt > 首次同步的 updatedAt（CI 低精度时钟容错）
	if err := store.DeleteNode(n.Id); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
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
	if _, err := store.UpsertEnvironment(model.Environment{
		WorkspaceId: wsId, Name: "dev",
		Variables: []model.Variable{
			{Key: "token", Value: "SECRET-1", Type: "secret", Enabled: true},
			{Key: "host", Value: "x.io", Type: "default", Enabled: true},
		},
	}); err != nil {
		t.Fatal(err)
	}
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
	var remote Snapshot
	if err := json.Unmarshal(data, &remote); err != nil {
		t.Fatal(err)
	}
	if !remote.SecretsOmitted || remote.SchemaVersion != snapshotSchemaVersion {
		t.Fatalf("secret omission metadata missing: %+v", remote)
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

func TestOmitSecretsCoversNodeAuthAndRestoresLocalValues(t *testing.T) {
	local := Snapshot{Nodes: []SyncNode{{Node: model.Node{
		Id: "n1",
		Request: &model.HttpRequest{Auth: model.Auth{Type: "bearer", Params: map[string]string{
			"token": "request-token",
		}}},
		Auth: &model.Auth{Type: "basic", Params: map[string]string{
			"username": "alice",
			"password": "node-password",
		}},
		Variables: []model.Variable{
			{Key: "secret", Value: "node-variable", Type: "secret", Enabled: true},
			{Key: "region", Value: "cn-north-1", Type: "default", Enabled: true},
		},
	}}}}
	raw, _ := json.Marshal(local)
	var upload Snapshot
	_ = json.Unmarshal(raw, &upload)
	stripSecrets(&upload)
	uploadRaw, _ := json.Marshal(upload)
	for _, value := range []string{"request-token", "node-password", "node-variable"} {
		if contains(uploadRaw, value) {
			t.Fatalf("node secret %q leaked: %s", value, uploadRaw)
		}
	}
	if !contains(uploadRaw, "alice") || !contains(uploadRaw, "cn-north-1") {
		t.Fatalf("non-secret data was removed: %s", uploadRaw)
	}
	restoreLocalSecrets(&upload, &local)
	node := upload.Nodes[0].Node
	if node.Request.Auth.Params["token"] != "request-token" || node.Auth.Params["password"] != "node-password" || node.Variables[0].Value != "node-variable" {
		t.Fatalf("node secrets not restored: %+v", node)
	}
}

func TestRestoreLocalSecretsUsesEntityIdAndDuplicateOccurrence(t *testing.T) {
	local := Snapshot{
		Environments: []model.Environment{
			{Id: "env-1", Name: "old-name", Variables: []model.Variable{
				{Key: "token", Value: "first-env-secret", Type: "secret", Enabled: true},
				{Key: "token", Value: "second-env-secret", Type: "secret", Enabled: true},
			}},
			{Id: "env-2", Name: "renamed", Variables: []model.Variable{
				{Key: "token", Value: "wrong-environment-secret", Type: "secret", Enabled: true},
			}},
		},
		Globals: []model.Variable{
			{Key: "token", Value: "first-global-secret", Type: "secret", Enabled: true},
			{Key: "token", Value: "second-global-secret", Type: "secret", Enabled: true},
		},
	}
	merged := Snapshot{
		Environments: []model.Environment{{Id: "env-1", Name: "renamed", Variables: []model.Variable{
			{Key: "token", Type: "secret", Enabled: true},
			{Key: "token", Type: "secret", Enabled: true},
		}}},
		Globals: []model.Variable{
			{Key: "token", Type: "secret", Enabled: true},
			{Key: "token", Type: "secret", Enabled: true},
		},
	}

	restoreLocalSecrets(&merged, &local)
	envVariables := merged.Environments[0].Variables
	if envVariables[0].Value != "first-env-secret" || envVariables[1].Value != "second-env-secret" {
		t.Fatalf("environment secrets restored by unstable identity: %+v", envVariables)
	}
	if merged.Globals[0].Value != "first-global-secret" || merged.Globals[1].Value != "second-global-secret" {
		t.Fatalf("global duplicate secrets restored incorrectly: %+v", merged.Globals)
	}
}

func TestRestoreRemoteOmittedSecretsDoesNotReviveExplicitClear(t *testing.T) {
	local := Snapshot{Environments: []model.Environment{{
		Id: "env-1", UpdatedAt: 1,
		Variables: []model.Variable{{Key: "token", Value: "local-secret", Type: "secret", Enabled: true}},
	}}}
	remote := Snapshot{
		SchemaVersion: snapshotSchemaVersion,
		Environments: []model.Environment{{
			Id: "env-1", UpdatedAt: 2,
			Variables: []model.Variable{{Key: "token", Value: "", Type: "secret", Enabled: true}},
		}},
	}
	merged, _ := merge(&local, &remote)
	restoreRemoteOmittedSecrets(merged, &local, &remote)
	if got := merged.Environments[0].Variables[0].Value; got != "" {
		t.Fatalf("explicitly cleared remote secret was revived: %q", got)
	}

	remote.SecretsOmitted = true
	merged, _ = merge(&local, &remote)
	restoreRemoteOmittedSecrets(merged, &local, &remote)
	if got := merged.Environments[0].Variables[0].Value; got != "local-secret" {
		t.Fatalf("omitted remote secret was not restored: %q", got)
	}
}

func TestValidateAndOrderSyncNodes(t *testing.T) {
	nodes := []SyncNode{
		{Node: model.Node{Id: "request", ParentId: "folder", Kind: "request"}},
		{Node: model.Node{Id: "folder", ParentId: "collection", Kind: "folder"}},
		{Node: model.Node{Id: "collection", Kind: "collection"}},
	}
	ordered, err := validateAndOrderSyncNodes(nodes)
	if err != nil {
		t.Fatal(err)
	}
	if ordered[0].Id != "collection" || ordered[1].Id != "folder" || ordered[2].Id != "request" {
		t.Fatalf("unexpected node order: %+v", ordered)
	}

	invalid := map[string][]SyncNode{
		"duplicate id": {
			{Node: model.Node{Id: "same", Kind: "collection"}},
			{Node: model.Node{Id: "same", Kind: "collection"}},
		},
		"missing parent": {
			{Node: model.Node{Id: "request", ParentId: "missing", Kind: "request"}},
		},
		"parent cycle": {
			{Node: model.Node{Id: "one", ParentId: "two", Kind: "folder"}},
			{Node: model.Node{Id: "two", ParentId: "one", Kind: "folder"}},
		},
		"live child under tombstone": {
			{Node: model.Node{Id: "collection", Kind: "collection"}, DeletedAt: 10},
			{Node: model.Node{Id: "request", ParentId: "collection", Kind: "request"}},
		},
	}
	for name, snapshot := range invalid {
		t.Run(name, func(t *testing.T) {
			if _, err := validateAndOrderSyncNodes(snapshot); err == nil {
				t.Fatal("invalid sync graph was accepted")
			}
		})
	}
}

func TestApplyToLocalPreflightsOwnershipBeforeWriting(t *testing.T) {
	store, workspaceId := newDevice(t)
	foreignWorkspace, err := store.CreateWorkspace("foreign")
	if err != nil {
		t.Fatal(err)
	}
	foreignNode, err := store.UpsertNode(model.Node{
		WorkspaceId: foreignWorkspace.Id,
		Kind:        "collection",
		Name:        "foreign",
	})
	if err != nil {
		t.Fatal(err)
	}
	merged := &Snapshot{
		SchemaVersion: snapshotSchemaVersion,
		Nodes: []SyncNode{
			{Node: model.Node{Id: "would-be-partial", Kind: "collection", Name: "new"}},
			{Node: model.Node{Id: foreignNode.Id, Kind: "collection", Name: "collision"}},
		},
	}
	if err := applyToLocal(store, workspaceId, merged); err == nil {
		t.Fatal("cross-workspace ID collision was accepted")
	}
	nodes, err := store.ListNodes(workspaceId)
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes {
		if node.Id == "would-be-partial" {
			t.Fatal("snapshot wrote nodes before ownership preflight completed")
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

func TestSyncUsesInjectedHTTPClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Network-Policy") != "shared" {
			http.Error(w, "missing shared client", http.StatusTeapot)
			return
		}
		switch r.Method {
		case http.MethodGet:
			http.NotFound(w, r)
		case http.MethodPut, "MKCOL":
			w.WriteHeader(http.StatusCreated)
		default:
			http.Error(w, "unsupported", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()
	client := &http.Client{Transport: syncRoundTripper(func(request *http.Request) (*http.Response, error) {
		request.Header.Set("X-Network-Policy", "shared")
		return http.DefaultTransport.RoundTrip(request)
	})}
	store, workspaceID := newDevice(t)
	if _, err := SyncWithClient(store, workspaceID, DavConfig{Url: server.URL}, client); err != nil {
		t.Fatal(err)
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
