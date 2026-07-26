package mock

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"apirequest/backend/model"
)

func testNodesAndExamples() ([]model.Node, []model.Example) {
	nodes := []model.Node{
		{Id: "col", Kind: "collection", Name: "c"},
		{Id: "r1", ParentId: "col", Kind: "request", Name: "get user",
			Request: &model.HttpRequest{Method: "GET", Url: "https://api.x.io/users/{{id}}"}},
		{Id: "r2", ParentId: "col", Kind: "request", Name: "list users",
			Request: &model.HttpRequest{Method: "GET", Url: "https://api.x.io/users"}},
		{Id: "r3", ParentId: "col", Kind: "request", Name: "create user",
			Request: &model.HttpRequest{Method: "POST", Url: "https://api.x.io/users"}},
	}
	examples := []model.Example{
		{Id: "e1", NodeId: "r1", Name: "one user", Status: 200,
			Headers: []model.KV{{Key: "Content-Type", Value: "application/json"}},
			Body:    `{"id":42}`},
		{Id: "e2", NodeId: "r2", Name: "user list", Status: 200, Body: `[{"id":1}]`},
		{Id: "e3", NodeId: "r3", Name: "created", Status: 201, Body: `{"ok":true}`},
		{Id: "e4", NodeId: "r3", Name: "conflict", Status: 409, Body: `{"error":"dup"}`},
	}
	return nodes, examples
}

func startTestServer(t *testing.T) (*Manager, *Server) {
	t.Helper()
	m := NewManager()
	nodes, examples := testNodesAndExamples()
	srv, err := m.Start("col", nodes, examples, Options{}, nil)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { m.StopAll() })
	return m, srv
}

func get(t *testing.T, url string, headers map[string]string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest("GET", url, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(body)
}

func TestExactAndWildcardMatch(t *testing.T) {
	_, srv := startTestServer(t)

	// 字面路径优先于通配
	resp, body := get(t, srv.Addr+"/users", nil)
	if resp.StatusCode != 200 || body != `[{"id":1}]` {
		t.Errorf("list = %d %s", resp.StatusCode, body)
	}
	// 通配段 {{id}}
	resp, body = get(t, srv.Addr+"/users/99", nil)
	if resp.StatusCode != 200 || body != `{"id":42}` {
		t.Errorf("get by id = %d %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %s", ct)
	}
}

func TestMethodMatch(t *testing.T) {
	_, srv := startTestServer(t)
	resp, body := postJSON(t, srv.Addr+"/users")
	if resp.StatusCode != 201 || body != `{"ok":true}` {
		t.Errorf("post = %d %s", resp.StatusCode, body)
	}
}

func postJSON(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Post(url, "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(body)
}

func TestExampleSelectionHeaders(t *testing.T) {
	_, srv := startTestServer(t)

	req, _ := http.NewRequest("POST", srv.Addr+"/users", nil)
	req.Header.Set("x-mock-response-code", "409")
	resp, _ := http.DefaultClient.Do(req)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 409 || string(body) != `{"error":"dup"}` {
		t.Errorf("by code = %d %s", resp.StatusCode, body)
	}

	req2, _ := http.NewRequest("POST", srv.Addr+"/users", nil)
	req2.Header.Set("x-mock-response-name", "created")
	resp2, _ := http.DefaultClient.Do(req2)
	resp2.Body.Close()
	if resp2.StatusCode != 201 {
		t.Errorf("by name = %d", resp2.StatusCode)
	}
}

func TestNoMatch404WithCandidates(t *testing.T) {
	_, srv := startTestServer(t)
	resp, body := get(t, srv.Addr+"/nope/deep/path", nil)
	if resp.StatusCode != 404 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var payload map[string]any
	json.Unmarshal([]byte(body), &payload)
	if payload["candidates"] == nil {
		t.Errorf("404 should list candidates: %s", body)
	}
}

func TestStartStopLifecycle(t *testing.T) {
	m, srv := startTestServer(t)
	if len(m.Running()) != 1 {
		t.Errorf("running = %v", m.Running())
	}
	m.Stop("col")
	if len(m.Running()) != 0 {
		t.Error("should be stopped")
	}
	// 停止后连接应失败
	if _, err := http.Get(srv.Addr + "/users"); err == nil {
		t.Error("server should be down")
	}
}

func TestNoExamplesError(t *testing.T) {
	m := NewManager()
	nodes := []model.Node{
		{Id: "col", Kind: "collection"},
		{Id: "r1", ParentId: "col", Kind: "request", Name: "r",
			Request: &model.HttpRequest{Method: "GET", Url: "https://x.io/a"}},
	}
	if _, err := m.Start("col", nodes, nil, Options{}, nil); err == nil {
		t.Error("no examples should error")
	}
}
