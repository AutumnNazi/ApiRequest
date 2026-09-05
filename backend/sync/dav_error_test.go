package sync

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// 真实网络故障（连接被拒）不得被报成 "sync canceled"
func TestDavRealNetworkErrorIsNotReportedAsCanceled(t *testing.T) {
	c, err := newDavClient(DavConfig{Url: "http://127.0.0.1:1/"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, gerr := c.Get(context.Background(), "x.json")
	if gerr == nil {
		t.Fatal("expected a network error")
	}
	t.Logf("GET err = %v", gerr)
	if strings.Contains(gerr.Error(), "canceled") {
		t.Errorf("BUG: real network failure reported as canceled: %v", gerr)
	}
}

// 父 ctx 被取消时仍须报 "sync canceled"（修复不能把这条弄丢）
func TestDavParentCancelIsReportedAsCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.Write([]byte("{}"))
	}))
	defer srv.Close()
	c, err := newDavClient(DavConfig{Url: srv.URL + "/"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(150*time.Millisecond, cancel)
	_, _, gerr := c.Get(ctx, "x.json")
	if gerr == nil {
		// handler 睡 3s、cancel 在 150ms：能拿到完整响应只可能是取消没传播
		// （等到 3s 后服务端才写回）。这里跳过等于把本用例要防的回归静默放行
		t.Fatal("GET completed despite parent cancel — cancellation did not propagate")
	}
	t.Logf("canceled GET err = %v", gerr)
	if !strings.Contains(gerr.Error(), "canceled") {
		t.Errorf("parent cancel should report canceled, got: %v", gerr)
	}
}
