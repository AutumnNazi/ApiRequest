package binding

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"apirequest/backend/httpengine"
	"apirequest/backend/model"
	"apirequest/backend/runner"
)

// pm.setNextRequest(name) 串行流转：A 的测试脚本指定下一个跑 C，B 被跳过
func TestRunCollectionSetNextRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	store := openRunnerStore(t)
	ws, _ := store.EnsureDefaultWorkspace()
	col, _ := store.UpsertNode(model.Node{WorkspaceId: ws.Id, Kind: "collection", Name: "flow"})
	mkReq := func(name, testScript string) {
		store.UpsertNode(model.Node{
			WorkspaceId: ws.Id, ParentId: col.Id, Kind: "request", Name: name,
			Request: &model.HttpRequest{
				Method: "GET", Url: srv.URL, Settings: model.DefaultSettings(),
				TestScript: testScript,
			},
		})
	}
	mkReq("A", `pm.setNextRequest('C');`)
	mkReq("B", "")
	mkReq("C", "")

	engine := httpengine.New()
	engine.SetBlobsDir(store.BlobsDir())
	runnerApi := NewRunnerApi(NewRequestApi(engine, store), store)

	report, err := runnerApi.RunCollection("flow-run", ws.Id, col.Id, runner.Options{Iterations: 1})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var names []string
	for _, rr := range report.Results {
		names = append(names, rr.RequestName)
	}
	// A → C，跳过 B；B 不产生结果条目
	if got := strings.Join(names, ","); got != "A,C" {
		t.Fatalf("execution order = %q, want 'A,C'", got)
	}
	if report.Total != 2 || report.Passed != 2 {
		t.Fatalf("report = %+v", report)
	}
}

// 自环防死循环：A 每次都 setNextRequest('A')，按步数上限终止
func TestRunCollectionSetNextRequestSelfLoopTerminates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	store := openRunnerStore(t)
	ws, _ := store.EnsureDefaultWorkspace()
	col, _ := store.UpsertNode(model.Node{WorkspaceId: ws.Id, Kind: "collection", Name: "loop"})
	store.UpsertNode(model.Node{
		WorkspaceId: ws.Id, ParentId: col.Id, Kind: "request", Name: "A",
		Request: &model.HttpRequest{
			Method: "GET", Url: srv.URL, Settings: model.DefaultSettings(),
			TestScript: `pm.setNextRequest('A');`,
		},
	})

	engine := httpengine.New()
	engine.SetBlobsDir(store.BlobsDir())
	runnerApi := NewRunnerApi(NewRequestApi(engine, store), store)

	report, err := runnerApi.RunCollection("loop-run", ws.Id, col.Id, runner.Options{Iterations: 1})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Total != runnerMaxNextRequestHops {
		t.Fatalf("total = %d, want hop cap %d (must terminate)", report.Total, runnerMaxNextRequestHops)
	}
}

// 未知名：回到自然顺序（从下一个未执行的请求继续）
func TestRunCollectionSetNextRequestUnknownName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	store := openRunnerStore(t)
	ws, _ := store.EnsureDefaultWorkspace()
	col, _ := store.UpsertNode(model.Node{WorkspaceId: ws.Id, Kind: "collection", Name: "unknown"})
	mkReq := func(name, testScript string) {
		store.UpsertNode(model.Node{
			WorkspaceId: ws.Id, ParentId: col.Id, Kind: "request", Name: name,
			Request: &model.HttpRequest{
				Method: "GET", Url: srv.URL, Settings: model.DefaultSettings(),
				TestScript: testScript,
			},
		})
	}
	mkReq("A", `pm.setNextRequest('nope');`)
	mkReq("B", "")

	engine := httpengine.New()
	engine.SetBlobsDir(store.BlobsDir())
	runnerApi := NewRunnerApi(NewRequestApi(engine, store), store)

	report, err := runnerApi.RunCollection("unknown-run", ws.Id, col.Id, runner.Options{Iterations: 1})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var names []string
	for _, rr := range report.Results {
		names = append(names, rr.RequestName)
	}
	if got := strings.Join(names, ","); got != "A,B" {
		t.Fatalf("execution order = %q, want 'A,B' (unknown name falls back to natural order)", got)
	}
}

// 多轮迭代：跳转不跨迭代行——下一轮从树序第一个请求重新开始
func TestRunCollectionSetNextRequestDoesNotCrossIterations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	store := openRunnerStore(t)
	ws, _ := store.EnsureDefaultWorkspace()
	col, _ := store.UpsertNode(model.Node{WorkspaceId: ws.Id, Kind: "collection", Name: "iters"})
	mkReq := func(name, testScript string) {
		store.UpsertNode(model.Node{
			WorkspaceId: ws.Id, ParentId: col.Id, Kind: "request", Name: name,
			Request: &model.HttpRequest{
				Method: "GET", Url: srv.URL, Settings: model.DefaultSettings(),
				TestScript: testScript,
			},
		})
	}
	mkReq("A", `pm.setNextRequest('C');`)
	mkReq("B", "")
	mkReq("C", "")

	engine := httpengine.New()
	engine.SetBlobsDir(store.BlobsDir())
	runnerApi := NewRunnerApi(NewRequestApi(engine, store), store)

	report, err := runnerApi.RunCollection("iter-run", ws.Id, col.Id, runner.Options{Iterations: 2})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	// 每轮 A→C，共 4 条；按 (iteration, execution order) 检验
	want := "1:A,1:C,2:A,2:C"
	var got []string
	for _, rr := range report.Results {
		got = append(got, fmt.Sprintf("%d:%s", rr.Iteration, rr.RequestName))
	}
	if strings.Join(got, ",") != want {
		t.Fatalf("execution = %q, want %q", strings.Join(got, ","), want)
	}
}
