package binding

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"apirequest/backend/httpengine"
	"apirequest/backend/mock"
	"apirequest/backend/model"
	"apirequest/backend/runner"
	"apirequest/backend/secrets"
	"apirequest/backend/storage"
)

func TestCollectionOperationsIgnoreUnrelatedLockedSecrets(t *testing.T) {
	dir := t.TempDir()
	vault := secrets.NewWithKeyring(dir, nil)
	if err := vault.Unlock("subtree-test"); err != nil {
		t.Fatal(err)
	}
	store, err := storage.OpenWithVault(dir, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	workspace, err := store.EnsureDefaultWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	collection, err := store.UpsertNode(model.Node{WorkspaceId: workspace.Id, Kind: "collection", Name: "public"})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }))
	defer srv.Close()
	folder, err := store.UpsertNode(model.Node{WorkspaceId: workspace.Id, ParentId: collection.Id, Kind: "folder", Name: "nested"})
	if err != nil {
		t.Fatal(err)
	}
	request, err := store.UpsertNode(model.Node{WorkspaceId: workspace.Id, ParentId: folder.Id, Kind: "request", Name: "public-request", Request: &model.HttpRequest{Method: "GET", Url: srv.URL + "/public", Settings: model.DefaultSettings()}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.UpsertExample(model.Example{NodeId: request.Id, Name: "ok", Status: 201, Body: "mock-body"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.UpsertNode(model.Node{WorkspaceId: workspace.Id, Kind: "collection", Name: "unrelated-private", Auth: &model.Auth{Type: "bearer", Params: map[string]string{"token": "private-secret"}}})
	if err != nil {
		t.Fatal(err)
	}
	vault.Lock()
	t.Run("runner", func(t *testing.T) {
		engine := httpengine.New()
		engine.SetBlobsDir(store.BlobsDir())
		report, err := NewRunnerApi(NewRequestApi(engine, store), store).RunCollection("public-run", workspace.Id, collection.Id, runner.Options{Iterations: 1})
		if err != nil {
			t.Fatal(err)
		}
		if report.Total != 1 || report.Passed != 1 {
			t.Fatalf("report = %+v", report)
		}
	})
	t.Run("export", func(t *testing.T) {
		data, err := NewConvertApi(store).ExportData(collection.Id, "postman")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(data, "public-request") || strings.Contains(data, "unrelated-private") {
			t.Fatalf("unexpected export: %s", data)
		}
	})
	t.Run("mock", func(t *testing.T) {
		api := NewMockApi(store, mock.NewManager())
		status, err := api.StartMockServer(collection.Id, mock.Options{})
		if err != nil {
			t.Fatal(err)
		}
		defer api.StopMockServer(collection.Id)
		response, err := http.Get(status.Addr + "/public")
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != 201 {
			t.Fatalf("mock status = %d", response.StatusCode)
		}
	})
}

func TestListNodesReturnsSummariesWithoutUnlockingVault(t *testing.T) {
	dir := t.TempDir()
	vault := secrets.NewWithKeyring(dir, nil)
	if err := vault.Unlock("node-summary-test"); err != nil {
		t.Fatal(err)
	}
	store, err := storage.OpenWithVault(dir, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	workspace, _ := store.EnsureDefaultWorkspace()
	collection, err := store.UpsertNode(model.Node{WorkspaceId: workspace.Id, Kind: "collection", Name: "secured"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.UpsertNode(model.Node{
		WorkspaceId: workspace.Id, ParentId: collection.Id, Kind: "request", Name: "private",
		Request: &model.HttpRequest{
			Method: "POST", Url: "https://example.test", Body: model.Body{Kind: "raw", Text: "private-body"},
			Auth:     model.Auth{Type: "bearer", Params: map[string]string{"token": "private-token"}},
			Settings: model.DefaultSettings(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	vault.Lock()

	nodes, err := NewNodeApi(store, nil).ListNodes(workspace.Id)
	if err != nil {
		t.Fatalf("list summaries while Vault is locked: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("summaries = %d, want 2", len(nodes))
	}
	raw, err := json.Marshal(nodes)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{"private-body", "private-token", `"request":`, `"auth":`, `"variables":`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("node summary contains %q: %s", forbidden, text)
		}
	}
}

func TestDeleteWorkspaceCancelsRequestsAndReleasesLiveBlobs(t *testing.T) {
	requestStarted := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/large":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(strings.Repeat("x", (2<<20)+1024)))
		case "/wait":
			close(requestStarted)
			<-r.Context().Done()
		}
	}))
	defer srv.Close()

	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	workspace, _ := store.EnsureDefaultWorkspace()
	if _, err := store.CreateWorkspace("keep"); err != nil {
		t.Fatal(err)
	}
	engine := httpengine.New()
	engine.SetBlobsDir(store.BlobsDir())
	requestApi := NewRequestApi(engine, store)
	nodeApi := NewNodeApi(store, requestApi)

	large, err := requestApi.SendRequest(
		"workspace-live-blob",
		model.HttpRequest{Method: "GET", Url: srv.URL + "/large", Settings: model.DefaultSettings()},
		model.SendContext{WorkspaceId: workspace.Id},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := requestApi.GetResponseBlobInfo(large.Body.BlobRef); err != nil {
		t.Fatalf("live blob unavailable before workspace deletion: %v", err)
	}

	requestDone := make(chan error, 1)
	go func() {
		_, sendErr := requestApi.SendRequest(
			"workspace-in-flight",
			model.HttpRequest{Method: "GET", Url: srv.URL + "/wait", Settings: model.DefaultSettings()},
			model.SendContext{WorkspaceId: workspace.Id},
		)
		requestDone <- sendErr
	}()
	select {
	case <-requestStarted:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}

	if err := nodeApi.DeleteWorkspace(workspace.Id); err != nil {
		t.Fatal(err)
	}
	select {
	case sendErr := <-requestDone:
		if sendErr == nil || !strings.Contains(sendErr.Error(), "canceled") {
			t.Fatalf("request error after workspace deletion = %v", sendErr)
		}
	case <-time.After(time.Second):
		t.Fatal("workspace deletion did not drain the request")
	}
	if _, err := requestApi.GetResponseBlobInfo(large.Body.BlobRef); err == nil {
		t.Fatal("workspace live blob remained available after deletion")
	}
	workspaces, err := store.ListWorkspaces()
	if err != nil || len(workspaces) != 1 || workspaces[0].Id == workspace.Id {
		t.Fatalf("workspaces after deletion = %+v, err = %v", workspaces, err)
	}
}
