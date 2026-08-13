package binding

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"apirequest/backend/httpengine"
	"apirequest/backend/model"
	"apirequest/backend/runner"
	"apirequest/backend/storage"
)

// TestFullLifecycle 验证：环境变量解析 + 前置脚本设变量改请求 +
// 测试脚本断言 + 脚本变量持久化 的完整管线
func TestFullLifecycle(t *testing.T) {
	var gotPath, gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Get("X-Trace")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"user":"alice"}`))
	}))
	defer srv.Close()

	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	w, _ := store.EnsureDefaultWorkspace()

	// 环境：base + uid
	env, _ := store.UpsertEnvironment(model.Environment{
		WorkspaceId: w.Id, Name: "dev", IsActive: true,
		Variables: []model.Variable{
			{Key: "base", Value: srv.URL, Type: "default", Enabled: true},
			{Key: "uid", Value: "42", Type: "default", Enabled: true},
		},
	})
	store.SetActiveEnvironment(w.Id, env.Id)

	// 集合 + 请求节点（集合级前置脚本，验证继承）
	col, _ := store.UpsertNode(model.Node{
		WorkspaceId: w.Id, Kind: "collection", Name: "c",
		PreScript: `pm.environment.set('trace', 'from-collection');`,
	})
	reqNode, _ := store.UpsertNode(model.Node{
		WorkspaceId: w.Id, ParentId: col.Id, Kind: "request", Name: "r",
	})

	req := model.HttpRequest{
		Method: "GET",
		Url:    "{{base}}/users/{{uid}}",
		Headers: []model.KV{
			{Key: "X-Trace", Value: "{{trace}}", Enabled: true},
		},
		Settings: model.DefaultSettings(),
		TestScript: `
			pm.test('status ok', function () { pm.expect(pm.response.code).to.equal(200); });
			pm.test('json user', function () { pm.expect(pm.response.json().user).to.equal('alice'); });
			pm.environment.set('lastUser', pm.response.json().user);
			console.log('done');
		`,
	}

	api := NewRequestApi(httpengine.New(), store)
	res, err := api.SendRequest("s1", req, model.SendContext{
		WorkspaceId: w.Id, RequestId: reqNode.Id,
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}

	// 变量解析：URL 与 header（trace 来自集合级前置脚本）
	if gotPath != "/users/42" {
		t.Errorf("path = %q, want /users/42", gotPath)
	}
	if gotHeader != "from-collection" {
		t.Errorf("X-Trace = %q, want from-collection", gotHeader)
	}

	// 测试断言
	if len(res.TestResults) != 2 || !res.TestResults[0].Pass || !res.TestResults[1].Pass {
		t.Errorf("testResults = %+v", res.TestResults)
	}
	if len(res.ScriptLogs) != 1 || res.ScriptLogs[0] != "done" {
		t.Errorf("logs = %v", res.ScriptLogs)
	}

	// 脚本变量持久化：trace 与 lastUser 都应写回环境
	envAfter, _ := store.GetEnvironment(env.Id)
	vars := map[string]string{}
	for _, v := range envAfter.Variables {
		vars[v.Key] = v.Value
	}
	if vars["trace"] != "from-collection" || vars["lastUser"] != "alice" {
		t.Errorf("persisted env vars = %v", vars)
	}

	// 历史快照应为已解析请求
	page, _ := store.ListHistory(w.Id, model.HistoryQuery{})
	if len(page.Items) != 1 {
		t.Fatalf("history count = %d", len(page.Items))
	}
	detail, err := store.GetHistory(w.Id, page.Items[0].Id)
	if err != nil || detail.RequestSnap.Url != srv.URL+"/users/42" {
		t.Errorf("history snap url = %q, err = %v", detail.RequestSnap.Url, err)
	}
	if len(detail.TestResults) != 2 {
		t.Errorf("history testResults = %+v", detail.TestResults)
	}
}

func TestScriptChangesToExistingSecretVariablesAreRedacted(t *testing.T) {
	const rotatedSecret = "rotated-script-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	workspace, _ := store.EnsureDefaultWorkspace()
	env, err := store.UpsertEnvironment(model.Environment{
		WorkspaceId: workspace.Id,
		Name:        "secret-env",
		IsActive:    true,
		Variables: []model.Variable{
			{Key: "token", Value: "old-script-secret", Type: "secret", Enabled: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetActiveEnvironment(workspace.Id, env.Id); err != nil {
		t.Fatal(err)
	}

	response, err := NewRequestApi(httpengine.New(), store).SendRequest(
		"script-secret-rotation",
		model.HttpRequest{
			Method: "GET",
			Url:    srv.URL,
			TestScript: `
				pm.environment.set('token', '` + rotatedSecret + `');
				console.log('token=' + pm.environment.get('token'));
				pm.test('token=' + pm.environment.get('token'), function () { pm.expect(false).to.equal(true); });
			`,
			Settings: model.DefaultSettings(),
		},
		model.SendContext{WorkspaceId: workspace.Id, EnvironmentId: env.Id},
	)
	if err != nil {
		t.Fatal(err)
	}
	scriptRaw, err := json.Marshal(struct {
		Logs  []string           `json:"logs"`
		Tests []model.TestResult `json:"tests"`
	}{Logs: response.ScriptLogs, Tests: response.TestResults})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(scriptRaw), rotatedSecret) {
		t.Fatalf("rotated secret leaked in script output: %s", scriptRaw)
	}

	updated, err := store.GetEnvironment(env.Id)
	if err != nil || len(updated.Variables) != 1 || updated.Variables[0].Value != rotatedSecret {
		t.Fatalf("rotated secret was not persisted: %+v, err = %v", updated.Variables, err)
	}
	detail, err := store.GetHistory(workspace.Id, response.HistoryId)
	if err != nil {
		t.Fatal(err)
	}
	historyRaw, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(historyRaw), rotatedSecret) {
		t.Fatalf("rotated secret leaked in history: %s", historyRaw)
	}
}

// TestPreScriptFailureAborts 前置脚本抛错应中止发送
func TestPreScriptFailureAborts(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
	}))
	defer srv.Close()

	store, _ := storage.Open(t.TempDir())
	defer store.Close()
	w, _ := store.EnsureDefaultWorkspace()

	api := NewRequestApi(httpengine.New(), store)
	_, err := api.SendRequest("s1", model.HttpRequest{
		Method: "GET", Url: srv.URL, Settings: model.DefaultSettings(),
		PreScript: `throw new Error('abort me')`,
	}, model.SendContext{WorkspaceId: w.Id})

	if err == nil {
		t.Fatal("want error")
	}
	ae, ok := err.(*model.AppError)
	if !ok || ae.Kind != model.KindScript || ae.Phase != "pre" {
		t.Errorf("err = %v", err)
	}
	if hit {
		t.Error("request should not be sent after pre-script failure")
	}
}

func TestRunnerCancelStopsCurrentRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	workspace, _ := store.EnsureDefaultWorkspace()
	collection, _ := store.UpsertNode(model.Node{
		WorkspaceId: workspace.Id, Kind: "collection", Name: "cancel",
	})
	node, _ := store.UpsertNode(model.Node{
		WorkspaceId: workspace.Id,
		ParentId:    collection.Id,
		Kind:        "request",
		Name:        "slow",
		Request: &model.HttpRequest{
			Method: "GET", Url: srv.URL, Settings: model.DefaultSettings(),
		},
	})
	requestApi := NewRequestApi(httpengine.New(), store)
	runnerApi := NewRunnerApi(requestApi, store)
	type result struct {
		reportCanceled bool
		failed         int
		err            error
	}
	done := make(chan result, 1)
	go func() {
		report, runErr := runnerApi.RunCollection("cancel-run", workspace.Id, collection.Id, runner.Options{})
		out := result{err: runErr}
		if report != nil {
			out.reportCanceled = report.Canceled
			out.failed = report.Failed
		}
		done <- out
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("runner request did not start")
	}
	if err := runnerApi.CancelRun("cancel-run"); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-done:
		if got.err != nil || !got.reportCanceled || got.failed != 0 {
			t.Fatalf("canceled run = %+v", got)
		}
	case <-time.After(500 * time.Millisecond):
		_ = requestApi.CancelRequest("cancel-run-" + node.Id)
		t.Fatal("CancelRun did not stop the current HTTP request")
	}
}
