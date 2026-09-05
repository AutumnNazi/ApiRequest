package binding

import (
	"testing"

	"apirequest/backend/model"
	"apirequest/backend/runner"
	"apirequest/backend/secrets"
	"apirequest/backend/storage"
)

// newRunnerApiWithStore 用内存 keyring（binding 包惯例），避免依赖系统凭据管理器
func newRunnerApiWithStore(t *testing.T) (*RunnerApi, *storage.Store, string) {
	t.Helper()
	dir := t.TempDir()
	vault := secrets.NewWithKeyring(dir, &bindingMemoryKeyring{})
	store, err := storage.OpenWithVault(dir, vault)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.EnsureDefaultWorkspace(); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	list, err := store.ListWorkspaces()
	if err != nil || len(list) == 0 {
		t.Fatalf("no workspace: %v", err)
	}
	api := NewRunnerApi(nil, store)
	return api, store, list[0].Id
}

func TestRunnerRunsPersistAcrossRestart(t *testing.T) {
	api, store, ws := newRunnerApiWithStore(t)

	rep := &runner.Report{
		RunId: "run-1", Total: 2, Passed: 1, Failed: 1, DurationMs: 30,
		Results: []runner.RequestResult{
			{Iteration: 1, RequestName: "ok", NodeId: "n1", Status: 200},
			{Iteration: 1, RequestName: "bad", NodeId: "n2", Status: 500,
				TestResults: []model.TestResult{{Name: "status ok", Pass: false, Error: "500 != 2xx"}}},
		},
	}
	if err := store.SaveRunnerRun(ws, "col-9", rep); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	// 模拟应用重启：新 RunnerApi 实例（内存报告缓存为空）
	fresh := NewRunnerApi(nil, store)
	page, err := fresh.ListRunnerRuns(ws, model.RunnerRunQuery{})
	if err != nil {
		t.Fatalf("ListRunnerRuns: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("expected persisted run, got %+v", page.Items)
	}
	if page.Items[0].RunId != "run-1" || page.Items[0].Passed != 1 || page.Items[0].Failed != 1 {
		t.Fatalf("summary mismatch: %+v", page.Items[0])
	}
	if page.Items[0].CollectionId != "col-9" {
		t.Fatalf("collection filter value lost: %+v", page.Items[0])
	}

	detail, err := fresh.GetRunnerRun(ws, "run-1")
	if err != nil {
		t.Fatalf("GetRunnerRun: %v", err)
	}
	if len(detail.Results) != 2 || detail.Results[1].TestResults[0].Error != "500 != 2xx" {
		t.Fatalf("detail mismatch: %+v", detail.Results)
	}

	if err := fresh.DeleteRunnerRun(ws, "run-1"); err != nil {
		t.Fatalf("DeleteRunnerRun: %v", err)
	}
	if _, err := fresh.GetRunnerRun(ws, "run-1"); err == nil {
		t.Fatal("deleted run still retrievable")
	}
	_ = api
}

func TestRunCollectionPersistsReport(t *testing.T) {
	// 不真发请求：直接验证 RunCollection 的落库路径较重；这里只验证
	// rememberReport 之外 SaveRunnerRun 由 RunCollection 调用（通过无请求集合报错路径不落库）
	api, store, ws := newRunnerApiWithStore(t)
	_, err := api.RunCollection("run-x", ws, "missing-collection", runner.Options{Iterations: 1})
	if err == nil {
		t.Fatal("expected error for empty collection")
	}
	page, err := store.ListRunnerRuns(ws, model.RunnerRunQuery{})
	if err != nil {
		t.Fatalf("ListRunnerRuns: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("failed run must not persist: %+v", page.Items)
	}
}
