package binding

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"apirequest/backend/httpengine"
	"apirequest/backend/model"
	"apirequest/backend/runner"
	"apirequest/backend/storage"
)

// 并发模式：服务端在途计数应达到并发度；串行则始终为 1
func TestRunCollectionConcurrency(t *testing.T) {
	var inflight, maxInflight atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := inflight.Add(1)
		for {
			old := maxInflight.Load()
			if cur <= old || maxInflight.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(80 * time.Millisecond)
		inflight.Add(-1)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	store := openRunnerStore(t)
	ws, _ := store.EnsureDefaultWorkspace()
	col, _ := store.UpsertNode(model.Node{WorkspaceId: ws.Id, Kind: "collection", Name: "perf"})
	store.UpsertNode(model.Node{
		WorkspaceId: ws.Id, ParentId: col.Id, Kind: "request", Name: "hit",
		Request: &model.HttpRequest{Method: "GET", Url: srv.URL, Settings: model.DefaultSettings()},
	})

	engine := httpengine.New()
	engine.SetBlobsDir(store.BlobsDir())
	runnerApi := NewRunnerApi(NewRequestApi(engine, store), store)

	// 串行：1 轮单请求无并发可言，2 轮 × 1 请求串行在途峰值 = 1
	report, err := runnerApi.RunCollection("serial-run", ws.Id, col.Id, runner.Options{Iterations: 2})
	if err != nil {
		t.Fatalf("serial run: %v", err)
	}
	if report.Passed != 2 {
		t.Fatalf("serial report = %+v", report)
	}
	if got := maxInflight.Load(); got != 1 {
		t.Fatalf("serial maxInflight = %d, want 1", got)
	}

	// 并发度 2：2 轮 × 1 请求，在途峰值应达到 2
	report, err = runnerApi.RunCollection("concurrent-run", ws.Id, col.Id, runner.Options{Iterations: 2, Concurrency: 2})
	if err != nil {
		t.Fatalf("concurrent run: %v", err)
	}
	if report.Passed != 2 {
		t.Fatalf("concurrent report = %+v", report)
	}
	if got := maxInflight.Load(); got < 2 {
		t.Fatalf("concurrent maxInflight = %d, want >= 2", got)
	}
}

// 并发下数据行仍按迭代绑定：每轮只看到本行的变量
func TestRunCollectionConcurrencyDataRows(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.URL.Path] = true
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer srv.Close()

	store := openRunnerStore(t)
	ws, _ := store.EnsureDefaultWorkspace()
	col, _ := store.UpsertNode(model.Node{WorkspaceId: ws.Id, Kind: "collection", Name: "perf-data"})
	store.UpsertNode(model.Node{
		WorkspaceId: ws.Id, ParentId: col.Id, Kind: "request", Name: "hit",
		Request: &model.HttpRequest{Method: "GET", Url: srv.URL + "/{{mark}}", Settings: model.DefaultSettings()},
	})

	engine := httpengine.New()
	engine.SetBlobsDir(store.BlobsDir())
	runnerApi := NewRunnerApi(NewRequestApi(engine, store), store)

	report, err := runnerApi.RunCollection("data-run", ws.Id, col.Id, runner.Options{
		DataFile:    "mark\nalpha\nbeta",
		DataFormat:  "csv",
		Concurrency: 2,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Passed != 2 {
		t.Fatalf("report = %+v", report)
	}
	mu.Lock()
	defer mu.Unlock()
	if !seen["/alpha"] || !seen["/beta"] {
		t.Fatalf("paths seen = %v, want /alpha and /beta", seen)
	}
}

// 并发 + stopOnError：失败后尽快停止，未启动的任务记为 skipped
func TestRunCollectionConcurrencyStopOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer srv.Close()

	store := openRunnerStore(t)
	ws, _ := store.EnsureDefaultWorkspace()
	col, _ := store.UpsertNode(model.Node{WorkspaceId: ws.Id, Kind: "collection", Name: "perf-stop"})
	store.UpsertNode(model.Node{
		WorkspaceId: ws.Id, ParentId: col.Id, Kind: "request", Name: "always-fails",
		Request: &model.HttpRequest{
			Method: "GET", Url: srv.URL, Settings: model.DefaultSettings(),
			TestScript: `pm.test('200', function(){ pm.expect(pm.response.code).to.equal(200); });`,
		},
	})

	engine := httpengine.New()
	engine.SetBlobsDir(store.BlobsDir())
	runnerApi := NewRunnerApi(NewRequestApi(engine, store), store)

	report, err := runnerApi.RunCollection("stop-run", ws.Id, col.Id, runner.Options{
		Iterations: 4, Concurrency: 2, StopOnError: true,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Failed == 0 {
		t.Fatalf("report = %+v, want failures", report)
	}
	if report.Total >= 4 {
		t.Fatalf("stopOnError should prevent some tasks: total=%d", report.Total)
	}
}

func openRunnerStore(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}
