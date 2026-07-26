package httpengine

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"apirequest/backend/model"
)

func testReq(url string) model.HttpRequest {
	return model.HttpRequest{
		Method:   "GET",
		Url:      url,
		Settings: model.DefaultSettings(),
	}
}

func TestSendGet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	res, err := New().Send(context.Background(), testReq(srv.URL))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if res.Status != 200 {
		t.Errorf("status = %d, want 200", res.Status)
	}
	if res.Body.Text != `{"ok":true}` {
		t.Errorf("body = %q", res.Body.Text)
	}
	if res.SizeBytes != 11 {
		t.Errorf("size = %d, want 11", res.SizeBytes)
	}
	if res.Timing.TotalMs <= 0 {
		t.Errorf("totalMs = %v, want > 0", res.Timing.TotalMs)
	}
	if res.Timing.TtfbMs <= 0 {
		t.Errorf("ttfbMs = %v, want > 0", res.Timing.TtfbMs)
	}
}

func TestSendPostJsonAndQueryMerge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %s", ct)
		}
		if r.URL.Query().Get("a") != "1" || r.URL.Query().Get("b") != "2" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "x" {
			t.Errorf("body = %v", body)
		}
		w.WriteHeader(201)
	}))
	defer srv.Close()

	req := model.HttpRequest{
		Method: "post",
		Url:    srv.URL + "?a=1",
		Params: []model.KV{
			{Key: "b", Value: "2", Enabled: true},
			{Key: "c", Value: "3", Enabled: false}, // 未启用，不应发送
		},
		Body:     model.Body{Kind: "raw", Language: "json", Text: `{"name":"x"}`},
		Settings: model.DefaultSettings(),
	}
	res, err := New().Send(context.Background(), req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if res.Status != 201 {
		t.Errorf("status = %d, want 201", res.Status)
	}
}

func TestCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_, err := New().Send(ctx, testReq(srv.URL))
	if err == nil {
		t.Fatal("want error after cancel")
	}
	ae, ok := err.(*model.AppError)
	if !ok || ae.Detail != "canceled" {
		t.Errorf("err = %v, want AppError{canceled}", err)
	}
}

func TestNoFollowRedirects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/next", http.StatusFound)
	}))
	defer srv.Close()

	req := testReq(srv.URL)
	req.Settings.FollowRedirects = false
	res, err := New().Send(context.Background(), req)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if res.Status != 302 {
		t.Errorf("status = %d, want 302", res.Status)
	}
}

func TestUrlencodedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.PostForm.Get("k") != "v" {
			t.Errorf("form = %v", r.PostForm)
		}
	}))
	defer srv.Close()

	req := model.HttpRequest{
		Method: "POST",
		Url:    srv.URL,
		Body: model.Body{Kind: "urlencoded", Items: []model.FormItem{
			{Key: "k", Value: "v", Enabled: true, Type: "text"},
		}},
		Settings: model.DefaultSettings(),
	}
	if _, err := New().Send(context.Background(), req); err != nil {
		t.Fatalf("send: %v", err)
	}
}

func TestTruncateLargeBody(t *testing.T) {
	big := strings.Repeat("x", inlineBodyLimit+1000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(big))
	}))
	defer srv.Close()

	res, err := New().Send(context.Background(), testReq(srv.URL))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if res.SizeBytes != int64(len(big)) {
		t.Errorf("size = %d, want %d (真实大小含被丢弃部分)", res.SizeBytes, len(big))
	}
	if !strings.Contains(res.Body.Text, "truncated") {
		t.Error("want truncation marker in body text")
	}
}

func TestInvalidUrl(t *testing.T) {
	_, err := New().Send(context.Background(), testReq("://bad"))
	if err == nil {
		t.Fatal("want error for invalid url")
	}
	if ae, ok := err.(*model.AppError); !ok || ae.Kind != model.KindValidation {
		t.Errorf("err = %v, want KindValidation", err)
	}
}
