package storage

import (
	"testing"

	"apirequest/backend/model"
	"apirequest/backend/runner"
)

// 保留策略设置化：history/runner_run 的保留上限从 setting 读取，未配置回落默认值
func TestRetentionLimitsFromSettings(t *testing.T) {
	store := openStoreForRetention(t)
	if got := store.historyRetention(); got != historyRetentionLimit {
		t.Fatalf("default history retention = %d, want %d", got, historyRetentionLimit)
	}
	if got := store.runnerRunRetention(); got != runnerRunRetentionLimit {
		t.Fatalf("default runner run retention = %d, want %d", got, runnerRunRetentionLimit)
	}

	if err := store.SetSettings(map[string]string{
		"retention.history":    "50",
		"retention.runnerRuns": "10",
	}); err != nil {
		t.Fatal(err)
	}
	if got := store.historyRetention(); got != 50 {
		t.Fatalf("history retention = %d, want 50", got)
	}
	if got := store.runnerRunRetention(); got != 10 {
		t.Fatalf("runner run retention = %d, want 10", got)
	}
}

// 非法值（0/负数/非数字）回落默认而不是拒绝写入或报错
func TestRetentionLimitsInvalidFallBackToDefaults(t *testing.T) {
	store := openStoreForRetention(t)
	if err := store.SetSettings(map[string]string{
		"retention.history":    "abc",
		"retention.runnerRuns": "-5",
	}); err != nil {
		t.Fatal(err)
	}
	if got := store.historyRetention(); got != historyRetentionLimit {
		t.Fatalf("history retention = %d, want default %d", got, historyRetentionLimit)
	}
	if got := store.runnerRunRetention(); got != runnerRunRetentionLimit {
		t.Fatalf("runner run retention = %d, want default %d", got, runnerRunRetentionLimit)
	}
}

// 收缩保留数后立即按新上限裁剪：history 8 次插入 → 上限 5 时旧行被清掉
func TestHistoryRetentionAppliesOnInsert(t *testing.T) {
	store := openStoreForRetention(t)
	ws, _ := store.EnsureDefaultWorkspace()
	if err := store.SetSettings(map[string]string{"retention.history": "5"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := store.InsertHistory(model.HistoryRecord{
			WorkspaceId: ws.Id,
			RequestSnap: model.HttpRequest{Method: "GET", Url: "https://x.test"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.ListHistory(ws.Id, model.HistoryQuery{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 5 {
		t.Fatalf("history rows = %d, want 5 (retention applied on insert)", len(page.Items))
	}
}

// runner_run 保留收缩：超过新上限的旧运行被裁掉
func TestRunnerRunRetentionAppliesOnSave(t *testing.T) {
	store := openStoreForRetention(t)
	ws, _ := store.EnsureDefaultWorkspace()
	col, _ := store.UpsertNode(model.Node{WorkspaceId: ws.Id, Kind: "collection", Name: "ret"})
	if err := store.SetSettings(map[string]string{"retention.runnerRuns": "3"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if err := store.SaveRunnerRun(ws.Id, col.Id, &runner.Report{
			RunId: "run-" + string(rune('a'+i)), Total: 1,
		}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := store.ListRunnerRuns(ws.Id, model.RunnerRunQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 3 {
		t.Fatalf("runner runs = %d, want 3 (retention applied on save)", len(page.Items))
	}
}

func openStoreForRetention(t *testing.T) *Store {
	t.Helper()
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
