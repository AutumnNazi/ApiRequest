package binding

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"apirequest/backend/httpengine"
	"apirequest/backend/model"
	"apirequest/backend/runner"
)

// DelayMs：串行模式下请求之间等待；不与任务执行重叠（对端按到达间隔观察）
func TestRunCollectionDelayBetweenRequests(t *testing.T) {
	var lastArrival time.Time
	gapMu := make(chan time.Duration, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		if !lastArrival.IsZero() {
			gap := now.Sub(lastArrival)
			select {
			case gapMu <- gap:
			default:
				// 只留第一次观测到的间隔
			}
		}
		lastArrival = now
		w.WriteHeader(200)
	}))
	defer srv.Close()

	store := openRunnerStore(t)
	ws, _ := store.EnsureDefaultWorkspace()
	col, _ := store.UpsertNode(model.Node{WorkspaceId: ws.Id, Kind: "collection", Name: "delay"})
	for i := 0; i < 3; i++ {
		store.UpsertNode(model.Node{
			WorkspaceId: ws.Id, ParentId: col.Id, Kind: "request", Name: "hit",
			Request: &model.HttpRequest{Method: "GET", Url: srv.URL, Settings: model.DefaultSettings()},
		})
	}

	engine := httpengine.New()
	engine.SetBlobsDir(store.BlobsDir())
	runnerApi := NewRunnerApi(NewRequestApi(engine, store), store)

	start := time.Now()
	report, err := runnerApi.RunCollection("delay-run", ws.Id, col.Id, runner.Options{
		Iterations: 1, DelayMs: 120,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Passed != 3 {
		t.Fatalf("report = %+v", report)
	}
	elapsed := time.Since(start)
	// 3 请求 2 个间隔：至少 240ms（本机调度波动余量后取 200ms 下限）
	if elapsed < 200*time.Millisecond {
		t.Fatalf("run elapsed = %v, want >= 200ms with 2 gaps of 120ms", elapsed)
	}
	// 单个间隔也必须 >= 100ms（taskCh 派发与执行的抖动容忍）
	var observed time.Duration
	select {
	case observed = <-gapMu:
	default:
	}
	if observed != 0 && observed < 100*time.Millisecond {
		t.Fatalf("observed inter-request gap = %v, too small", observed)
	}
}

// DelayMs + 并发模式：延迟作用于每个 worker 的任务间（不破坏并发度）
func TestRunCollectionDelayWithConcurrency(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	store := openRunnerStore(t)
	ws, _ := store.EnsureDefaultWorkspace()
	col, _ := store.UpsertNode(model.Node{WorkspaceId: ws.Id, Kind: "collection", Name: "cdelay"})
	for i := 0; i < 4; i++ {
		store.UpsertNode(model.Node{
			WorkspaceId: ws.Id, ParentId: col.Id, Kind: "request", Name: "hit",
			Request: &model.HttpRequest{Method: "GET", Url: srv.URL, Settings: model.DefaultSettings()},
		})
	}

	engine := httpengine.New()
	engine.SetBlobsDir(store.BlobsDir())
	runnerApi := NewRunnerApi(NewRequestApi(engine, store), store)

	report, err := runnerApi.RunCollection("cdelay-run", ws.Id, col.Id, runner.Options{
		Iterations: 1, DelayMs: 100, Concurrency: 2,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Passed != 4 {
		t.Fatalf("report = %+v", report)
	}
}
