package storage

import (
	"testing"

	"apirequest/backend/model"
	"apirequest/backend/runner"
)

func newRunnerRunTestStore(t *testing.T) *Store {
	t.Helper()
	s := openStoreWithMemoryKeyring(t, t.TempDir(), &memoryKeyring{values: map[string]string{}})
	if _, err := s.EnsureDefaultWorkspace(); err != nil {
		t.Fatalf("seed workspace: %v", err)
	}
	return s
}

func firstWorkspaceId(t *testing.T, s *Store) string {
	t.Helper()
	// 幂等：库为空时创建默认工作区（cookie jar 等测试需要归属）
	w, err := s.EnsureDefaultWorkspace()
	if err != nil {
		t.Fatalf("no workspace: %v", err)
	}
	return w.Id
}

func sampleReport(runId string, results ...runner.RequestResult) *runner.Report {
	return &runner.Report{
		RunId: runId, Total: len(results), Passed: len(results),
		DurationMs: 42, Results: results,
	}
}

func TestSaveAndGetRunnerRun(t *testing.T) {
	s := newRunnerRunTestStore(t)
	ws := firstWorkspaceId(t, s)
	rep := sampleReport("run-1",
		runner.RequestResult{Iteration: 1, RequestName: "list", NodeId: "n1", Status: 200, DurationMs: 12,
			TestResults: []model.TestResult{{Name: "status is 2xx", Pass: true}}},
	)
	if err := s.SaveRunnerRun(ws, "col-1", rep); err != nil {
		t.Fatalf("SaveRunnerRun: %v", err)
	}

	got, err := s.GetRunnerRun(ws, "run-1")
	if err != nil {
		t.Fatalf("GetRunnerRun: %v", err)
	}
	if got.RunId != "run-1" || got.Total != 1 || got.Passed != 1 || got.DurationMs != 42 {
		t.Fatalf("summary mismatch: %+v", got)
	}
	if len(got.Results) != 1 || got.Results[0].Status != 200 || got.Results[0].TestResults[0].Name != "status is 2xx" {
		t.Fatalf("results mismatch: %+v", got.Results)
	}
}

func TestGetRunnerRunRejectsForeignWorkspace(t *testing.T) {
	s := newRunnerRunTestStore(t)
	ws := firstWorkspaceId(t, s)
	if err := s.SaveRunnerRun(ws, "col-1", sampleReport("run-1")); err != nil {
		t.Fatalf("SaveRunnerRun: %v", err)
	}
	// 另一工作区拿不到：运行历史是 per-workspace 的边界
	if _, err := s.GetRunnerRun("ws-other", "run-1"); err == nil {
		t.Fatal("expected error for foreign workspace, got nil")
	}
}

func TestListRunnerRunsPaginatesNewestFirstWithoutResults(t *testing.T) {
	s := newRunnerRunTestStore(t)
	ws := firstWorkspaceId(t, s)
	for i := 0; i < 5; i++ {
		rep := sampleReport(string(rune('a' + i)))
		rep.Failed = i
		if err := s.SaveRunnerRun(ws, "col-1", rep); err != nil {
			t.Fatalf("SaveRunnerRun %d: %v", i, err)
		}
	}
	page1, err := s.ListRunnerRuns(ws, model.RunnerRunQuery{Limit: 2})
	if err != nil {
		t.Fatalf("ListRunnerRuns: %v", err)
	}
	if len(page1.Items) != 2 || page1.Items[0].RunId != "e" || page1.Items[1].RunId != "d" {
		t.Fatalf("page1 order/len mismatch: %+v", page1.Items)
	}
	if !page1.HasMore {
		t.Fatal("expected hasMore")
	}
	page2, err := s.ListRunnerRuns(ws, model.RunnerRunQuery{Limit: 2, Cursor: page1.NextCursor})
	if err != nil {
		t.Fatalf("ListRunnerRuns page2: %v", err)
	}
	if len(page2.Items) != 2 || page2.Items[0].RunId != "c" {
		t.Fatalf("page2 mismatch: %+v", page2.Items)
	}

	// 集合过滤
	filtered, err := s.ListRunnerRuns(ws, model.RunnerRunQuery{CollectionId: "col-2"})
	if err != nil {
		t.Fatalf("ListRunnerRuns filtered: %v", err)
	}
	if len(filtered.Items) != 0 {
		t.Fatalf("expected 0 for other collection: %+v", filtered.Items)
	}
}

func TestSaveRunnerRunIsIdempotentByRunId(t *testing.T) {
	s := newRunnerRunTestStore(t)
	ws := firstWorkspaceId(t, s)
	first := sampleReport("run-1")
	first.Passed = 1
	if err := s.SaveRunnerRun(ws, "col-1", first); err != nil {
		t.Fatalf("SaveRunnerRun: %v", err)
	}
	// 同 runId 重放（重试场景）不应产生重复行
	second := sampleReport("run-1")
	second.Passed = 0
	if err := s.SaveRunnerRun(ws, "col-1", second); err != nil {
		t.Fatalf("SaveRunnerRun retry: %v", err)
	}
	items, err := s.ListRunnerRuns(ws, model.RunnerRunQuery{})
	if err != nil {
		t.Fatalf("ListRunnerRuns: %v", err)
	}
	if len(items.Items) != 1 || items.Items[0].Passed != 0 {
		t.Fatalf("expected single updated row: %+v", items.Items)
	}
}

func TestDeleteAndClearRunnerRuns(t *testing.T) {
	s := newRunnerRunTestStore(t)
	ws := firstWorkspaceId(t, s)
	if err := s.SaveRunnerRun(ws, "col-1", sampleReport("run-1")); err != nil {
		t.Fatalf("SaveRunnerRun: %v", err)
	}
	if err := s.SaveRunnerRun(ws, "col-2", sampleReport("run-2")); err != nil {
		t.Fatalf("SaveRunnerRun: %v", err)
	}
	if err := s.DeleteRunnerRun(ws, "run-1"); err != nil {
		t.Fatalf("DeleteRunnerRun: %v", err)
	}
	if _, err := s.GetRunnerRun(ws, "run-1"); err == nil {
		t.Fatal("deleted run still retrievable")
	}
	if err := s.ClearRunnerRuns(ws); err != nil {
		t.Fatalf("ClearRunnerRuns: %v", err)
	}
	page, err := s.ListRunnerRuns(ws, model.RunnerRunQuery{})
	if err != nil {
		t.Fatalf("ListRunnerRuns: %v", err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("expected empty after clear: %+v", page.Items)
	}
}
