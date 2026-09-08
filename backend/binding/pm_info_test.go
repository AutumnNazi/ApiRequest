package binding

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"apirequest/backend/httpengine"
	"apirequest/backend/model"
	"apirequest/backend/runner"
	"apirequest/backend/secrets"
	"apirequest/backend/storage"
)

// TestRunCollectionPmInfo Runner 内 pm.info 可见迭代号/请求名/总轮数（数据驱动分支脚本用）
func TestRunCollectionPmInfo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	store := openRunnerStore(t)
	ws, _ := store.EnsureDefaultWorkspace()
	col, _ := store.UpsertNode(model.Node{WorkspaceId: ws.Id, Kind: "collection", Name: "info"})
	// 数据行标记迭代：iteration 值本身断言 pm.info.iteration 正确（1-based）
	script := `
		pm.test('info visible', function () {
			if (pm.info.iterationCount !== 2) throw new Error('iterationCount=' + pm.info.iterationCount);
			if (pm.info.requestName !== 'probe') throw new Error('requestName=' + pm.info.requestName);
			if (pm.info.eventName !== 'test') throw new Error('eventName=' + pm.info.eventName);
			if (String(pm.info.iteration) !== pm.variables.get('iteration')) {
				throw new Error('iteration=' + pm.info.iteration + ' row=' + pm.variables.get('iteration'));
			}
		});
	`
	store.UpsertNode(model.Node{
		WorkspaceId: ws.Id, ParentId: col.Id, Kind: "request", Name: "probe",
		Request: &model.HttpRequest{
			Method: "GET", Url: srv.URL, Settings: model.DefaultSettings(),
			TestScript: script,
		},
	})

	engine := httpengine.New()
	engine.SetBlobsDir(store.BlobsDir())
	runnerApi := NewRunnerApi(NewRequestApi(engine, store), store)
	report, err := runnerApi.RunCollection("info-run", ws.Id, col.Id, runner.Options{
		DataFile: `[{"iteration":"1"},{"iteration":"2"}]`, DataFormat: "json",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Passed != 2 || report.Failed != 0 {
		t.Fatalf("report = %+v", report)
	}
	for _, rr := range report.Results {
		for _, tr := range rr.TestResults {
			if !tr.Pass {
				t.Errorf("iter %d %s: %s", rr.Iteration, rr.RequestName, tr.Error)
			}
		}
	}
}

// TestSingleSendPmInfoDefaults 单发请求（非 Runner）时 pm.info 迭代语义为 1/1、请求名取节点名
func TestSingleSendPmInfoDefaults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	store := openRunnerStore(t)
	ws, _ := store.EnsureDefaultWorkspace()
	col, _ := store.UpsertNode(model.Node{WorkspaceId: ws.Id, Kind: "collection", Name: "single"})
	node, err := store.UpsertNode(model.Node{
		WorkspaceId: ws.Id, ParentId: col.Id, Kind: "request", Name: "solo",
		Request: &model.HttpRequest{
			Method: "GET", Url: srv.URL, Settings: model.DefaultSettings(),
			TestScript: `
				pm.test('info defaults', function () {
					if (pm.info.iteration !== 1) throw new Error('iteration=' + pm.info.iteration);
					if (pm.info.iterationCount !== 1) throw new Error('iterationCount=' + pm.info.iterationCount);
					if (pm.info.requestName !== 'solo') throw new Error('requestName=' + pm.info.requestName);
					if (pm.info.eventName !== 'test') throw new Error('eventName=' + pm.info.eventName);
				});
			`,
		},
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	engine := httpengine.New()
	engine.SetBlobsDir(store.BlobsDir())
	api := NewRequestApi(engine, store)
	res, err := api.SendRequest("send-info", *node.Request, model.SendContext{
		WorkspaceId: ws.Id, RequestId: node.Id,
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(res.TestResults) != 1 || !res.TestResults[0].Pass {
		t.Fatalf("test results = %+v", res.TestResults)
	}
}

// TestRunnerResponseToAssertions Runner 链路端到端验证 pm.response.to.be/have
func TestRunnerResponseToAssertions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	store := openRunnerStore(t)
	ws, _ := store.EnsureDefaultWorkspace()
	col, _ := store.UpsertNode(model.Node{WorkspaceId: ws.Id, Kind: "collection", Name: "resp-to"})
	store.UpsertNode(model.Node{
		WorkspaceId: ws.Id, ParentId: col.Id, Kind: "request", Name: "created",
		Request: &model.HttpRequest{
			Method: "GET", Url: srv.URL, Settings: model.DefaultSettings(),
			TestScript: `
				pm.test('created', function () { pm.response.to.be.created; });
				pm.test('status 201', function () { pm.response.to.have.status(201); });
			`,
		},
	})

	engine := httpengine.New()
	engine.SetBlobsDir(store.BlobsDir())
	runnerApi := NewRunnerApi(NewRequestApi(engine, store), store)
	report, err := runnerApi.RunCollection("to-run", ws.Id, col.Id, runner.Options{Iterations: 1})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Passed != 1 || report.Failed != 0 {
		t.Fatalf("report = %+v", report)
	}
}

// TestRunnerIterationData Runner 数据行经 pm.iterationData 暴露（数据驱动脚本标准入口）
func TestRunnerIterationData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	store := openRunnerStore(t)
	ws, _ := store.EnsureDefaultWorkspace()
	col, _ := store.UpsertNode(model.Node{WorkspaceId: ws.Id, Kind: "collection", Name: "iterdata"})
	store.UpsertNode(model.Node{
		WorkspaceId: ws.Id, ParentId: col.Id, Kind: "request", Name: "probe",
		Request: &model.HttpRequest{
			Method: "GET", Url: srv.URL, Settings: model.DefaultSettings(),
			TestScript: `
				pm.test('iterationData', function () {
					if (pm.iterationData.get('user') !== 'bob') throw new Error('get user');
					if (pm.iterationData.size() !== 2) throw new Error('size=' + pm.iterationData.size());
				});
			`,
		},
	})

	engine := httpengine.New()
	engine.SetBlobsDir(store.BlobsDir())
	runnerApi := NewRunnerApi(NewRequestApi(engine, store), store)
	report, err := runnerApi.RunCollection("iterdata-run", ws.Id, col.Id, runner.Options{
		DataFile: `[{"user":"bob","role":"dev"}]`, DataFormat: "json",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Passed != 1 || report.Failed != 0 {
		t.Fatalf("report = %+v", report)
	}
}

// TestSingleSendPmCookies 单发请求 pm.cookies 可读目标域的 Jar cookie
func TestSingleSendPmCookies(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	vault := secrets.NewWithKeyring(t.TempDir(), &bindingMemoryKeyring{})
	store, err := storage.OpenWithVault(t.TempDir(), vault)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ws, err := store.EnsureDefaultWorkspace()
	if err != nil {
		t.Fatal(err)
	}

	// Jar 里放两条：目标域一条 + 无关域一条（只该看到前者）。
	// 取 host 时去端口：httptest URL 是 127.0.0.1:port，cookie domain 不含端口
	su, _ := url.Parse(srv.URL)
	host := su.Hostname()
	if err := store.UpsertCookies(ws.Id, []model.Cookie{
		{Name: "session", Value: "jar-sess", Domain: host, Path: "/"},
		{Name: "other", Value: "nope", Domain: "unrelated.test", Path: "/"},
	}); err != nil {
		t.Fatalf("seed cookies: %v", err)
	}

	col, err := store.UpsertNode(model.Node{WorkspaceId: ws.Id, Kind: "collection", Name: "cookies"})
	if err != nil {
		t.Fatal(err)
	}
	node, err := store.UpsertNode(model.Node{
		WorkspaceId: ws.Id, ParentId: col.Id, Kind: "request", Name: "probe",
		Request: &model.HttpRequest{
			Method: "GET", Url: srv.URL, Settings: model.DefaultSettings(),
			TestScript: `
				pm.test('jar cookies', function () {
					if (pm.cookies.get('session') !== 'jar-sess') throw new Error('get session');
					if (pm.cookies.has('other')) throw new Error('unrelated domain leaked');
				});
			`,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	engine := httpengine.New()
	engine.SetBlobsDir(store.BlobsDir())
	api := NewRequestApi(engine, store)
	res, err := api.SendRequest("send-cookies", *node.Request, model.SendContext{
		WorkspaceId: ws.Id, RequestId: node.Id,
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(res.TestResults) != 1 || !res.TestResults[0].Pass {
		t.Fatalf("test results = %+v", res.TestResults)
	}
}
