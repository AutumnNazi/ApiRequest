package binding

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"apirequest/backend/httpengine"
	"apirequest/backend/secrets"
	"apirequest/backend/storage"
	appsync "apirequest/backend/sync"
)

// newScopeTestStore 建测试库：内存 keyring 而非 storage.Open——CI（Linux 无
// D-Bus secret service）下系统凭据管理器不可写，vault 锁死会让 SetSyncConfig
// 在到达被测逻辑前就报 storage 错误
func newScopeTestStore(t *testing.T) *storage.Store {
	t.Helper()
	vault := secrets.NewWithKeyring(t.TempDir(), &bindingMemoryKeyring{})
	store, err := storage.OpenWithVault(t.TempDir(), vault)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// 并发 SyncNow 的去重语义：同一 workspace 已有同步在跑时，第二次调用必须立刻
// 失败（operation already in flight），而不是排队各自完整跑一轮 GET→merge→PUT。
// 用阻塞 handler 做确定性会合：第一个同步卡在 GET 中（此刻必然在途），
// 第二次调用必须在此时被拒——不依赖两个 goroutine 的启动时序
func TestSyncNowRejectsConcurrentDuplicate(t *testing.T) {
	store := newScopeTestStore(t)
	ws, err := store.EnsureDefaultWorkspace()
	if err != nil {
		t.Fatal(err)
	}

	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() { releaseOnce.Do(func() { close(release) }) }
	gotGet := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			select {
			case gotGet <- struct{}{}:
			default:
			}
			<-release // 第一轮 GET 挂起，直到测试确认去重后放行
		}
		w.Write([]byte(`{}`)) // 无效快照：让第一个同步尽快以错误结束
	}))
	defer srv.Close()
	defer releaseAll() // LIFO：先放行 handler 再关服，Close 才不会永等

	api := NewSyncApi(store, httpengine.New(), nil)
	if err := api.SetSyncConfig(appsync.DavConfig{Url: srv.URL, Username: "u", Password: "p"}); err != nil {
		t.Fatal(err)
	}

	firstDone := make(chan error, 1)
	go func() {
		_, err := api.SyncNow(ws.Id)
		firstDone <- err
	}()

	select {
	case <-gotGet: // 第一个同步已进入 GET，operation 必然在途
	case <-time.After(5 * time.Second):
		t.Fatal("first sync did not reach the DAV server")
	}

	// 在途窗口内第二次调用：必须立即被拒
	_, err = api.SyncNow(ws.Id)
	if err == nil || !strings.Contains(err.Error(), "already in flight") {
		t.Fatalf("second SyncNow during in-flight sync = %v, want already in flight", err)
	}

	releaseAll()
	select {
	case err := <-firstDone:
		if err != nil && strings.Contains(err.Error(), "already in flight") {
			t.Errorf("first sync was wrongly rejected: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first sync did not finish after release")
	}
}

// DeleteWorkspace 的取消链路：cancelScope(workspaceId) 必须中断在途同步的 dav
// 请求并等待其退出。SyncApi 与 Request/Runner 共享同一注册表（app.go 接线），
// 这里直接对共享注册表走与 DeleteWorkspace 相同的调用链验证传播
func TestSyncNowIsInterruptedByWorkspaceScopeCancel(t *testing.T) {
	store := newScopeTestStore(t)
	ws, err := store.EnsureDefaultWorkspace()
	if err != nil {
		t.Fatal(err)
	}

	block := make(chan struct{})
	gotRequest := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case gotRequest <- struct{}{}:
		default:
		}
		// 挂起 GET 直到测试放行；客户端取消后 Write 失败返回，
		// close(block) 兜底防 Server.Close 永等（defer 顺序：先放行后关服）
		select {
		case <-block:
		case <-time.After(3 * time.Second):
		}
	}))
	// 挂起 GET 直到测试放行；客户端取消后 Write 失败返回。
	// defer 顺序（LIFO）：close(block) 先于 srv.Close 执行，Close 才不会永等
	defer srv.Close()
	defer close(block)

	shared := newOperationRegistry()
	syncApi := NewSyncApi(store, httpengine.New(), shared)
	if err := syncApi.SetSyncConfig(appsync.DavConfig{Url: srv.URL, Username: "u", Password: "p"}); err != nil {
		t.Fatal(err)
	}

	syncDone := make(chan error, 1)
	go func() {
		_, err := syncApi.SyncNow(ws.Id)
		syncDone <- err
	}()

	select {
	case <-gotRequest:
	case <-time.After(2 * time.Second):
		t.Fatal("sync did not reach the DAV server")
	}

	// 与 NodeApi.DeleteWorkspace 相同的 5s 窗口和 cancelScope 调用
	cancelCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shared.cancelScope(cancelCtx, ws.Id); err != nil {
		t.Fatalf("cancelScope: %v", err)
	}

	select {
	case err := <-syncDone:
		if err == nil {
			t.Error("interrupted sync returned nil error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelScope returned but sync goroutine did not finish (leak)")
	}
}
