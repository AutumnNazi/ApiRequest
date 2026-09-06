package main

// 跨平台冒烟（CI desktop 矩阵每台 runner 执行，docs/ops.md §冒烟）：
// 进程内走 CLI 同一条核心链路——平台数据目录解析（platform.EnsurePaths）、
// keychain 不可用时的 Vault 回退、SQLite 迁移、HTTP 引擎与 Runner 汇总。
// 纯 GUI 交互（文件对话框/静默更新）不在本测试范围（ops.md 已声明由交互式冒烟覆盖）。
import (
	"net/http"
	"net/http/httptest"
	"testing"

	"apirequest/backend/binding"
	"apirequest/backend/httpengine"
	"apirequest/backend/model"
	"apirequest/backend/runner"
)

func TestCliSmoke(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// 平台数据目录 + Vault 回退（CI 无系统 keychain，走本地加密回退路径）
	store, err := openStore(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	ws, err := store.EnsureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensure workspace: %v", err)
	}
	col, err := store.UpsertNode(model.Node{WorkspaceId: ws.Id, Kind: "collection", Name: "smoke"})
	if err != nil {
		t.Fatalf("upsert collection: %v", err)
	}
	if _, err := store.UpsertNode(model.Node{
		WorkspaceId: ws.Id, ParentId: col.Id, Kind: "request", Name: "ok",
		Request: &model.HttpRequest{
			Method: "GET", Url: srv.URL,
			Settings: model.DefaultSettings(),
			TestScript: `pm.test('200', function(){ pm.expect(pm.response.code).to.equal(200); });`,
		},
	}); err != nil {
		t.Fatalf("upsert request: %v", err)
	}

	wsId, colId, err := resolveTarget(store, ws.Id, "smoke")
	if err != nil {
		t.Fatalf("resolveTarget: %v", err)
	}

	engine := httpengine.New()
	engine.SetBlobsDir(store.BlobsDir())
	requestApi := binding.NewRequestApi(engine, store)
	runnerApi := binding.NewRunnerApi(requestApi, store)

	report, err := runnerApi.RunCollection("smoke-run", wsId, colId, runner.Options{})
	if err != nil {
		t.Fatalf("RunCollection: %v", err)
	}
	if report.Total != 1 || report.Passed != 1 || report.Failed != 0 {
		t.Fatalf("report = %+v, want 1 passed", report)
	}
}
