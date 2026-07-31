package binding

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"apirequest/backend/httpengine"
	"apirequest/backend/model"
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
