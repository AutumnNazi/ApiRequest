package graphql

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testRoundTripper func(*http.Request) (*http.Response, error)

func (fn testRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

// 用一个最小 GraphQL introspection mock 验证 Introspect 解析
func TestIntrospect(t *testing.T) {
	// 模拟 GraphQL 服务器：收到 query 后回 __schema
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 简单校验 POST + Content-Type
		if r.Method != "POST" || !strings.Contains(r.Header.Get("Content-Type"), "json") {
			http.Error(w, "bad request", 400)
			return
		}
		var body struct {
			Query string `json:"query"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		// 不严格匹配 query，直接回编排好的 schema
		_ = body // 仅断言有 query 字段
		resp := `{
  "data": {
    "__schema": {
      "queryType": {"name": "Query"},
      "mutationType": {"name": "Mutation"},
      "subscriptionType": null,
      "types": [
        {"kind": "OBJECT", "name": "Query", "fields": [
          {"name": "user", "description": "find a user", "type": {"kind": "OBJECT", "name": "User"}, "args": [
            {"name": "id", "type": {"kind": "NON_NULL", "ofType": {"kind": "SCALAR", "name": "ID"}}}
          ]}
        ]},
        {"kind": "OBJECT", "name": "User", "fields": [
          {"name": "id", "type": {"kind": "NON_NULL", "ofType": {"kind": "SCALAR", "name": "ID"}}},
          {"name": "name", "type": {"kind": "SCALAR", "name": "String"}}
        ]},
        {"kind": "OBJECT", "name": "Mutation", "fields": [
          {"name": "createUser", "type": {"kind": "OBJECT", "name": "User"}}
        ]}
      ],
      "directives": []
    }
  }
}`
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(resp))
	}))
	defer srv.Close()

	res, err := Introspect(context.Background(), IntrospectConfig{Url: srv.URL})
	if err != nil {
		t.Fatalf("introspect: %v", err)
	}
	if !strings.Contains(res.SchemaJSON, `"name":"Query"`) {
		t.Errorf("schema json missing Query type:\n%s", res.SchemaJSON)
	}
	if len(res.Queries) != 1 || res.Queries[0].Name != "user" {
		t.Errorf("queries = %+v", res.Queries)
	}
	if res.Queries[0].ReturnType != "User" {
		t.Errorf("return = %s", res.Queries[0].ReturnType)
	}
	if len(res.Mutations) != 1 || res.Mutations[0].Name != "createUser" {
		t.Errorf("mutations = %+v", res.Mutations)
	}
	// 非空类型渲染
	if !strings.Contains(res.Queries[0].Args, `"type"`) {
		t.Errorf("args json missing: %s", res.Queries[0].Args)
	}
}

func TestIntrospectErrors(t *testing.T) {
	// 401
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()
	if _, err := Introspect(context.Background(), IntrospectConfig{Url: srv.URL}); err == nil {
		t.Error("expected error on 401")
	}

	// GraphQL errors
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"errors":[{"message":"bad query"}]}`))
	}))
	defer srv2.Close()
	if _, err := Introspect(context.Background(), IntrospectConfig{Url: srv2.URL}); err == nil {
		t.Error("expected GraphQL error")
	}
}

func TestIntrospectRejectsOversizedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(strings.Repeat("x", maxIntrospectionResponseBytes+1)))
	}))
	defer srv.Close()

	if _, err := Introspect(context.Background(), IntrospectConfig{Url: srv.URL}); err == nil ||
		!strings.Contains(err.Error(), "response too large") {
		t.Fatalf("oversized response error = %v", err)
	}
}

func TestIntrospectUsesInjectedHTTPClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Network-Policy") != "shared" {
			http.Error(w, "missing shared client", http.StatusTeapot)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"__schema":{"types":[]}}}`))
	}))
	defer server.Close()
	client := &http.Client{Transport: testRoundTripper(func(request *http.Request) (*http.Response, error) {
		request.Header.Set("X-Network-Policy", "shared")
		return http.DefaultTransport.RoundTrip(request)
	})}
	if _, err := IntrospectWithClient(context.Background(), IntrospectConfig{Url: server.URL}, client); err != nil {
		t.Fatal(err)
	}
}
