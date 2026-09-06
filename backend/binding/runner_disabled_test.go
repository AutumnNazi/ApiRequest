package binding

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"apirequest/backend/httpengine"
	"apirequest/backend/model"
	"apirequest/backend/runner"
)

// 禁用请求被 Runner 跳过：不发送、不产生结果条目、计入 skipped
func TestRunCollectionSkipsDisabledRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	store := openRunnerStore(t)
	ws, _ := store.EnsureDefaultWorkspace()
	col, _ := store.UpsertNode(model.Node{WorkspaceId: ws.Id, Kind: "collection", Name: "mix"})
	mk := func(name string, disabled bool) {
		_, err := store.UpsertNode(model.Node{
			WorkspaceId: ws.Id, ParentId: col.Id, Kind: "request", Name: name, Disabled: disabled,
			Request: &model.HttpRequest{Method: "GET", Url: srv.URL, Settings: model.DefaultSettings()},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	mk("active", false)
	mk("paused", true)
	mk("alsoActive", false)

	engine := httpengine.New()
	engine.SetBlobsDir(store.BlobsDir())
	runnerApi := NewRunnerApi(NewRequestApi(engine, store), store)

	report, err := runnerApi.RunCollection("skip-run", ws.Id, col.Id, runner.Options{Iterations: 1})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Total != 2 || report.Passed != 2 {
		t.Fatalf("report = %+v (disabled must not run)", report)
	}
	if report.Skipped != 1 {
		t.Fatalf("skipped = %d, want 1 (disabled request counted as skipped)", report.Skipped)
	}
	for _, rr := range report.Results {
		if rr.RequestName == "paused" {
			t.Fatalf("disabled request executed: %+v", rr)
		}
	}
}
