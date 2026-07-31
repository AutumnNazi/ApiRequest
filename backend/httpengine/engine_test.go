package httpengine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

	// 无 blobsDir：截断
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

func TestLargeBodyToBlob(t *testing.T) {
	big := strings.Repeat("y", inlineBodyLimit+5000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(big))
	}))
	defer srv.Close()

	e := New()
	dir := t.TempDir()
	e.SetBlobsDir(dir)
	res, err := e.Send(context.Background(), testReq(srv.URL))
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if res.Body.Inline || res.Body.BlobRef == "" {
		t.Fatalf("body = %+v, want blob ref", res.Body)
	}
	if res.SizeBytes != int64(len(big)) {
		t.Errorf("size = %d, want %d", res.SizeBytes, len(big))
	}
	// blob 文件应为完整内容
	data, err := os.ReadFile(filepath.Join(dir, res.Body.BlobRef))
	if err != nil {
		t.Fatalf("read blob: %v", err)
	}
	if len(data) != len(big) || string(data[:10]) != "yyyyyyyyyy" {
		t.Errorf("blob len = %d, want %d", len(data), len(big))
	}
	// 内联部分应为预览片段
	if !strings.Contains(res.Body.Text, "预览片段") {
		t.Errorf("preview marker missing: %q", res.Body.Text[len(res.Body.Text)-60:])
	}
}

func TestSmallBodyStaysInline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("small"))
	}))
	defer srv.Close()
	e := New()
	e.SetBlobsDir(t.TempDir())
	res, err := e.Send(context.Background(), testReq(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Body.Inline || res.Body.BlobRef != "" || res.Body.Text != "small" {
		t.Errorf("body = %+v", res.Body)
	}
}

func TestGraphQLVariablesMustBeValidJSON(t *testing.T) {
	if _, _, err := buildBody(model.Body{Kind: "graphql", Variables: `{"broken":`}); err == nil {
		t.Fatal("invalid GraphQL variables should be rejected")
	}

	r, contentType, err := buildBody(model.Body{Kind: "graphql", Query: "query { ok }", Variables: `{"enabled":true}`})
	if err != nil {
		t.Fatalf("valid GraphQL variables rejected: %v", err)
	}
	if contentType != "application/json" {
		t.Fatalf("content type = %q, want application/json", contentType)
	}
	payload, err := io.ReadAll(r)
	if err != nil || !json.Valid(payload) {
		t.Fatalf("payload is not valid JSON: %q (%v)", payload, err)
	}
}

func TestLargeBodyBlobFailureStillMarksTruncation(t *testing.T) {
	big := strings.Repeat("z", inlineBodyLimit+1000)
	e := New()
	e.SetBlobsDir(filepath.Join(t.TempDir(), "missing"))
	head, total, blobRef, err := e.readBodyWithBlob(strings.NewReader(big))
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if blobRef != "" || total != int64(len(big)) {
		t.Fatalf("blobRef=%q total=%d, want empty ref and %d bytes", blobRef, total, len(big))
	}
	if !strings.Contains(string(head), "truncated") {
		t.Fatalf("truncation marker missing from fallback preview")
	}
}

func TestManualProxy(t *testing.T) {
	// 代理服务器：记录收到的请求并返回标记
	proxied := false
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied = true
		w.Write([]byte("via-proxy"))
	}))
	defer proxy.Close()

	e := New()
	if err := e.SetProxy("manual", proxy.URL); err != nil {
		t.Fatal(err)
	}
	res, err := e.Send(context.Background(), testReq("http://example.invalid/x"))
	if err != nil {
		t.Fatalf("send via proxy: %v", err)
	}
	if !proxied || res.Body.Text != "via-proxy" {
		t.Errorf("proxied=%v body=%q", proxied, res.Body.Text)
	}

	if err := e.SetProxy("manual", "::bad::"); err == nil {
		t.Error("invalid proxy url should error")
	}
	if err := e.SetProxy("none", ""); err != nil {
		t.Errorf("none: %v", err)
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
