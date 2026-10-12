package binding

import (
	"fmt"
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

// 多个在途请求一起失败或被取消时，每个 worker 都可能发出停止信号。
func TestRunCollectionSimultaneousWorkerStops(t *testing.T) {
	for _, cancelRun := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancel=%t", cancelRun), func(t *testing.T) {
			const workers = 8
			var arrivals atomic.Int32
			ready, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if arrivals.Add(1) == workers {
					close(ready)
				}
				select {
				case <-release:
				case <-r.Context().Done():
				}
				w.WriteHeader(500)
			}))
			defer srv.Close()
			defer unblock()
			store := openRunnerStore(t)
			ws, err := store.EnsureDefaultWorkspace()
			if err != nil {
				t.Fatal(err)
			}
			col, err := store.UpsertNode(model.Node{WorkspaceId: ws.Id, Kind: "collection", Name: "stop-together"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = store.UpsertNode(model.Node{WorkspaceId: ws.Id, ParentId: col.Id, Kind: "request", Name: "fail", Request: &model.HttpRequest{Method: "GET", Url: srv.URL, Settings: model.DefaultSettings(), TestScript: `pm.test('fail', function(){ pm.expect(pm.response.code).to.equal(200); });`}})
			if err != nil {
				t.Fatal(err)
			}
			engine := httpengine.New()
			engine.SetBlobsDir(store.BlobsDir())
			api := NewRunnerApi(NewRequestApi(engine, store), store)
			type outcome struct {
				report *runner.Report
				err    error
			}
			result := make(chan outcome, 1)
			go func() {
				report, err := api.RunCollection("stop-together", ws.Id, col.Id, runner.Options{Iterations: 32, Concurrency: workers, StopOnError: true})
				result <- outcome{report, err}
			}()
			defer api.CancelRun("stop-together")
			select {
			case <-ready:
			case <-time.After(10 * time.Second):
				t.Fatal("workers did not reach server")
			}
			if cancelRun {
				if err := api.CancelRun("stop-together"); err != nil {
					t.Fatal(err)
				}
			}
			unblock()
			select {
			case out := <-result:
				if out.err != nil {
					t.Fatal(out.err)
				}
				if out.report.Total >= 32 || out.report.Total+out.report.Skipped != 32 {
					t.Fatalf("unexpected report: %+v", out.report)
				}
				if cancelRun && !out.report.Canceled {
					t.Fatal("cancelled run was not marked cancelled")
				}
				if !cancelRun && out.report.Failed < workers {
					t.Fatalf("only %d workers recorded failure", out.report.Failed)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("workers failed to stop")
			}
		})
	}
}
