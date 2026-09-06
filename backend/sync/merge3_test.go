package sync

import (
	"encoding/json"
	"reflect"
	"testing"

	"apirequest/backend/model"
)

// ── 夹具 ──

func nodeWith(id, name, url string, updatedAt int64) SyncNode {
	return SyncNode{Node: model.Node{
		Id: id, Kind: "request", ParentId: "col-1", Name: name, UpdatedAt: updatedAt,
		Request: &model.HttpRequest{Method: "GET", Url: url, Settings: model.DefaultSettings()},
	}}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func requestURL(t *testing.T, n SyncNode) string {
	t.Helper()
	if n.Request == nil {
		t.Fatalf("node %s has no request", n.Id)
	}
	return n.Request.Url
}

func findNode(t *testing.T, snap *Snapshot, id string) SyncNode {
	t.Helper()
	for _, n := range snap.Nodes {
		if n.Id == id {
			return n
		}
	}
	t.Fatalf("node %s missing from merged snapshot", id)
	return SyncNode{}
}

// ── 节点字段级合并 ──

// 双端改同一请求的不同字段：都保留，无冲突
func TestMergeThreeWayNodeDifferentFields(t *testing.T) {
	base := &Snapshot{Nodes: []SyncNode{nodeWith("n1", "login", "https://x.test/old", 100)}}
	local := &Snapshot{Nodes: []SyncNode{nodeWith("n1", "login", "https://x.test/local", 200)}}
	remote := &Snapshot{Nodes: []SyncNode{nodeWith("n1", "renamed", "https://x.test/old", 150)}}

	merged, report, conflicts := mergeThreeWay(local, remote, base)
	got := findNode(t, merged, "n1")
	if got.Name != "renamed" {
		t.Fatalf("name = %q, want remote rename", got.Name)
	}
	if requestURL(t, got) != "https://x.test/local" {
		t.Fatalf("url = %q, want local edit", requestURL(t, got))
	}
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %+v", conflicts)
	}
	// 合并结果对两侧都有变更（本地拿到改名、远端拿到 URL 修改）→ 双向各计一次
	if report.Pushed != 1 || report.Pulled != 1 {
		t.Fatalf("report = %+v, want pushed=1 pulled=1", report)
	}
}

// 双端改同一字段：确定性本地胜出 + 冲突记录
func TestMergeThreeWaySameFieldConflict(t *testing.T) {
	base := &Snapshot{Nodes: []SyncNode{nodeWith("n1", "login", "https://x.test/old", 100)}}
	local := &Snapshot{Nodes: []SyncNode{nodeWith("n1", "login", "https://x.test/local", 200)}}
	remote := &Snapshot{Nodes: []SyncNode{nodeWith("n1", "login", "https://x.test/remote", 150)}}

	merged, _, conflicts := mergeThreeWay(local, remote, base)
	got := findNode(t, merged, "n1")
	if requestURL(t, got) != "https://x.test/local" {
		t.Fatalf("url = %q, want local wins", requestURL(t, got))
	}
	if len(conflicts) != 1 || conflicts[0].Field != "request.url" || conflicts[0].EntityId != "n1" {
		t.Fatalf("conflicts = %+v", conflicts)
	}
	if conflicts[0].LocalValue == "" || conflicts[0].RemoteValue == "" {
		t.Fatalf("conflict values missing: %+v", conflicts[0])
	}
}

// request 内部逐键合并：headers 数组整体为一个键
func TestMergeThreeWayRequestSubFields(t *testing.T) {
	baseNode := nodeWith("n1", "login", "https://x.test/old", 100)
	baseNode.Request.Headers = []model.KV{{Key: "A", Value: "1", Enabled: true}}
	localNode := baseNode
	localNode.UpdatedAt = 200
	localNode.Request = &model.HttpRequest{Method: "GET", Url: "https://x.test/old",
		Headers: []model.KV{{Key: "A", Value: "2", Enabled: true}}, Settings: model.DefaultSettings()}
	remoteNode := baseNode
	remoteNode.UpdatedAt = 150
	remoteNode.Request = &model.HttpRequest{Method: "POST", Url: "https://x.test/old",
		Headers: []model.KV{{Key: "A", Value: "1", Enabled: true}}, Settings: model.DefaultSettings()}

	base := &Snapshot{Nodes: []SyncNode{baseNode}}
	local := &Snapshot{Nodes: []SyncNode{localNode}}
	remote := &Snapshot{Nodes: []SyncNode{remoteNode}}

	merged, _, conflicts := mergeThreeWay(local, remote, base)
	got := findNode(t, merged, "n1")
	if got.Request.Method != "POST" {
		t.Fatalf("method = %q, want remote POST", got.Request.Method)
	}
	if !reflect.DeepEqual(got.Request.Headers, []model.KV{{Key: "A", Value: "2", Enabled: true}}) {
		t.Fatalf("headers = %+v, want local edit", got.Request.Headers)
	}
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %+v", conflicts)
	}
}

// 墓碑晚于编辑：删除胜出，不做内容合并
func TestMergeThreeWayTombstoneWins(t *testing.T) {
	baseNode := nodeWith("n1", "login", "https://x.test/old", 100)
	localNode := nodeWith("n1", "login", "https://x.test/local", 200)
	remoteNode := baseNode
	remoteNode.DeletedAt = 300 // 远端删除（更新）

	merged, _, _ := mergeThreeWay(
		&Snapshot{Nodes: []SyncNode{localNode}},
		&Snapshot{Nodes: []SyncNode{remoteNode}},
		&Snapshot{Nodes: []SyncNode{baseNode}},
	)
	got := findNode(t, merged, "n1")
	if got.DeletedAt != 300 {
		t.Fatalf("DeletedAt = %d, want tombstone 300", got.DeletedAt)
	}
	if requestURL(t, got) == "https://x.test/local" {
		t.Fatal("deleted node must not absorb local content edits")
	}
}

// 移动为结构性 LWW：本地移动（rev 更新）+ 远端改 URL → 父节点取本地、URL 取远端
func TestMergeThreeWayMoveBeatsEdit(t *testing.T) {
	baseNode := nodeWith("n1", "login", "https://x.test/old", 100)
	localNode := nodeWith("n1", "login", "https://x.test/old", 300)
	localNode.ParentId = "col-2" // 本地移动
	remoteNode := nodeWith("n1", "login", "https://x.test/remote", 200)

	merged, _, conflicts := mergeThreeWay(
		&Snapshot{Nodes: []SyncNode{localNode}},
		&Snapshot{Nodes: []SyncNode{remoteNode}},
		&Snapshot{Nodes: []SyncNode{baseNode}},
	)
	got := findNode(t, merged, "n1")
	if got.ParentId != "col-2" {
		t.Fatalf("parentId = %q, want local move", got.ParentId)
	}
	if requestURL(t, got) != "https://x.test/remote" {
		t.Fatalf("url = %q, want remote edit", requestURL(t, got))
	}
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %+v", conflicts)
	}
}

// 双端重排：冲突 + 本地胜出
func TestMergeThreeWaySortOrderConflict(t *testing.T) {
	baseNode := nodeWith("n1", "login", "https://x.test/old", 100)
	localNode := baseNode
	localNode.SortOrder = 20
	localNode.UpdatedAt = 200
	remoteNode := baseNode
	remoteNode.SortOrder = 30
	remoteNode.UpdatedAt = 150

	merged, _, conflicts := mergeThreeWay(
		&Snapshot{Nodes: []SyncNode{localNode}},
		&Snapshot{Nodes: []SyncNode{remoteNode}},
		&Snapshot{Nodes: []SyncNode{baseNode}},
	)
	if got := findNode(t, merged, "n1"); got.SortOrder != 20 {
		t.Fatalf("sortOrder = %v, want local", got.SortOrder)
	}
	if len(conflicts) != 1 || conflicts[0].Field != "sortOrder" {
		t.Fatalf("conflicts = %+v", conflicts)
	}
}

// 单端独有实体保持既有语义
func TestMergeThreeWayExclusiveEntities(t *testing.T) {
	local := &Snapshot{Nodes: []SyncNode{nodeWith("only-local", "a", "u1", 1)}}
	remote := &Snapshot{Nodes: []SyncNode{nodeWith("only-remote", "b", "u2", 2)}}
	base := &Snapshot{}

	merged, report, conflicts := mergeThreeWay(local, remote, base)
	if _, err := json.Marshal(merged); err != nil {
		t.Fatal(err)
	}
	findNode(t, merged, "only-local")
	findNode(t, merged, "only-remote")
	if report.Pushed == 0 || report.Pulled == 0 {
		t.Fatalf("report = %+v", report)
	}
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %+v", conflicts)
	}
}

// ── 环境 ──

// 本地改变量值、远端改环境名：都保留
func TestMergeThreeWayEnvironmentFields(t *testing.T) {
	mkEnv := func(name string, value string, updatedAt int64) model.Environment {
		return model.Environment{
			Id: "env-1", WorkspaceId: "ws", Name: name, UpdatedAt: updatedAt,
			Variables: []model.Variable{{Key: "BASE", Value: value, Enabled: true}},
		}
	}
	base := &Snapshot{Environments: []model.Environment{mkEnv("dev", "old", 100)}}
	local := &Snapshot{Environments: []model.Environment{mkEnv("dev", "local", 200)}}
	remote := &Snapshot{Environments: []model.Environment{mkEnv("development", "old", 150)}}

	merged, _, conflicts := mergeThreeWay(local, remote, base)
	if len(merged.Environments) != 1 {
		t.Fatalf("envs = %d", len(merged.Environments))
	}
	env := merged.Environments[0]
	if env.Name != "development" || env.Variables[0].Value != "local" {
		t.Fatalf("env = %+v", env)
	}
	if env.UpdatedAt != 200 {
		t.Fatalf("updatedAt = %d, want max(200,150)", env.UpdatedAt)
	}
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts: %+v", conflicts)
	}
}

// ── 冲突上限与计数 ──

func TestMergeThreeWayConflictCap(t *testing.T) {
	baseNodes := make([]SyncNode, 0, 150)
	localNodes := make([]SyncNode, 0, 150)
	remoteNodes := make([]SyncNode, 0, 150)
	for i := 0; i < 150; i++ {
		id := string(rune('A'+i/26)) + string(rune('a'+i%26)) + "-node-id"
		baseNodes = append(baseNodes, nodeWith(id, "n", "https://x.test/old", 100))
		localNodes = append(localNodes, nodeWith(id, "n", "https://x.test/local", 200))
		remoteNodes = append(remoteNodes, nodeWith(id, "n", "https://x.test/remote", 150))
	}
	_, _, conflicts := mergeThreeWay(
		&Snapshot{Nodes: localNodes},
		&Snapshot{Nodes: remoteNodes},
		&Snapshot{Nodes: baseNodes},
	)
	if len(conflicts) != conflictListCap {
		t.Fatalf("conflict list = %d, want capped at %d", len(conflicts), conflictListCap)
	}
}

// ── 工作区名 ──

func TestMergeThreeWayWorkspaceName(t *testing.T) {
	local := &Snapshot{WorkspaceName: "old"}
	remote := &Snapshot{WorkspaceName: "renamed"}
	base := &Snapshot{WorkspaceName: "old"}
	merged, _, _ := mergeThreeWay(local, remote, base)
	if merged.WorkspaceName != "renamed" {
		t.Fatalf("workspace name = %q", merged.WorkspaceName)
	}
	// 双端改名冲突：本地胜出
	local.WorkspaceName = "mine"
	remote.WorkspaceName = "theirs"
	var conflicts []SyncConflict
	merged, _, conflicts = mergeThreeWay(local, remote, base)
	if merged.WorkspaceName != "mine" || len(conflicts) == 0 {
		t.Fatalf("name = %q conflicts = %+v", merged.WorkspaceName, conflicts)
	}
}

// 序列化往返：合并结果必须是合法快照（applyToLocal 的前置校验可通过）
func TestMergeThreeWayResultValid(t *testing.T) {
	col := SyncNode{Node: model.Node{Id: "col-1", Kind: "collection", Name: "c", UpdatedAt: 1}}
	base := &Snapshot{Nodes: []SyncNode{col, nodeWith("n1", "login", "https://x.test/old", 100)}}
	local := &Snapshot{Nodes: []SyncNode{col, nodeWith("n1", "login", "https://x.test/local", 200)}}
	remote := &Snapshot{Nodes: []SyncNode{col, nodeWith("n1", "renamed", "https://x.test/old", 150)}}
	merged, _, _ := mergeThreeWay(local, remote, base)
	if err := validateSnapshot(merged); err != nil {
		t.Fatalf("merged snapshot invalid: %v", err)
	}
	if !json.Valid([]byte(mustJSON(t, merged))) {
		t.Fatal("merged snapshot not valid json")
	}
}

// ── 引擎级：两台设备 + 真实 WebDAV 的字段级合并 ──

// 设备 A 与 B 各改同一请求的不同字段：字段级合并后两端都保全；
// 收敛后重复同步不再产生实体变更，且两端都写入基线。
func TestSyncWithBaseMergesFieldsAcrossDevices(t *testing.T) {
	srv := startDav(t, "", "")
	cfg := DavConfig{Url: srv.URL}

	storeA, wsA := newDevice(t)
	storeB, _ := newDevice(t)
	// B 绑定同一远端工作区 id（现实里新设备以相同 workspaceId 接入同一快照）
	if err := storeB.EnsureWorkspace(wsA, "bound"); err != nil {
		t.Fatal(err)
	}
	wsB := wsA

	colA, _ := storeA.UpsertNode(model.Node{WorkspaceId: wsA, Kind: "collection", Name: "api"})
	nodeA, _ := storeA.UpsertNode(model.Node{
		WorkspaceId: wsA, ParentId: colA.Id, Kind: "request", Name: "login",
		Request: &model.HttpRequest{Method: "GET", Url: "https://x.test/old", Settings: model.DefaultSettings()},
	})
	if _, err := Sync(storeA, wsA, cfg); err != nil {
		t.Fatalf("A initial sync: %v", err)
	}
	if _, err := Sync(storeB, wsB, cfg); err != nil {
		t.Fatalf("B initial sync: %v", err)
	}

	// A 改 URL；B 改名（互不相干的字段）
	draftA, err := storeA.GetNode(wsA, nodeA.Id)
	if err != nil {
		t.Fatalf("A GetNode: %v", err)
	}
	draftA.Request.Url = "https://x.test/from-a"
	draftA.UpdatedAt = 1000
	if _, err := storeA.UpsertNode(draftA); err != nil {
		t.Fatal(err)
	}
	draftB, err := storeB.GetNode(wsB, nodeA.Id)
	if err != nil {
		t.Fatalf("B GetNode: %v", err)
	}
	draftB.Name = "login-renamed"
	draftB.UpdatedAt = 1100
	if _, err := storeB.UpsertNode(draftB); err != nil {
		t.Fatal(err)
	}

	if _, err := Sync(storeA, wsA, cfg); err != nil {
		t.Fatalf("A sync2: %v", err)
	}
	if _, err := Sync(storeB, wsB, cfg); err != nil {
		t.Fatalf("B sync2: %v", err)
	}

	// B 本地：URL 来自 A、名字保留 B 的改名
	nodeB, _ := storeB.GetNode(wsB, nodeA.Id)
	if nodeB.Request.Url != "https://x.test/from-a" {
		t.Fatalf("B url = %q, want A's edit", nodeB.Request.Url)
	}
	if nodeB.Name != "login-renamed" {
		t.Fatalf("B name = %q, want B's rename", nodeB.Name)
	}

	// A 再同步：拿到 B 的改名、保住自己的 URL
	if _, err := Sync(storeA, wsA, cfg); err != nil {
		t.Fatalf("A sync3: %v", err)
	}
	nodeA2, _ := storeA.GetNode(wsA, nodeA.Id)
	if nodeA2.Name != "login-renamed" {
		t.Fatalf("A name = %q, want B's rename", nodeA2.Name)
	}
	if nodeA2.Request.Url != "https://x.test/from-a" {
		t.Fatalf("A url = %q, want own edit", nodeA2.Request.Url)
	}

	// 收敛：再各一轮，无实体级变更
	repA3, err := Sync(storeA, wsA, cfg)
	if err != nil {
		t.Fatalf("A sync4: %v", err)
	}
	repB3, err := Sync(storeB, wsB, cfg)
	if err != nil {
		t.Fatalf("B sync4: %v", err)
	}
	if repA3.Pushed != 0 || repA3.Pulled != 0 || repB3.Pushed != 0 || repB3.Pulled != 0 {
		t.Fatalf("not converged: A=%+v B=%+v", repA3, repB3)
	}
	if _, _, ok, _ := storeA.GetSyncBase(wsA); !ok {
		t.Fatal("A missing sync base")
	}
	if _, _, ok, _ := storeB.GetSyncBase(wsB); !ok {
		t.Fatal("B missing sync base")
	}
}
