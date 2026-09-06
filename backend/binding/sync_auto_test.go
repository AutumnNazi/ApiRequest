package binding

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"apirequest/backend/httpengine"
	"apirequest/backend/secrets"
	"apirequest/backend/storage"
	appsync "apirequest/backend/sync"
)

func newAutoSyncApi(t *testing.T) (*SyncApi, *storage.Store, string) {
	t.Helper()
	dir := t.TempDir()
	vault := secrets.NewWithKeyring(dir, &bindingMemoryKeyring{})
	store, err := storage.OpenWithVault(dir, vault)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ws, err := store.EnsureDefaultWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	api := NewSyncApi(store, httpengine.New(), nil)
	return api, store, ws.Id
}

func TestAutoSyncSchedulerTriggersOnInterval(t *testing.T) {
	var davCalls atomic.Int64
	dav := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GET 快照返回最小合法 JSON；其余请求 200 即可——本测试只断言"被触发"
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{}`))
			davCalls.Add(1)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer dav.Close()

	api, _, wsId := newAutoSyncApi(t)
	if err := api.SetSyncConfig(appsync.DavConfig{
		Url: dav.URL, Username: "u", Password: "p", IntervalMinutes: 15,
	}); err != nil {
		t.Fatal(err)
	}
	_ = wsId

	stop := api.StartAutoSync(time.Millisecond * 50)
	defer stop()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if davCalls.Load() > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("auto sync did not trigger within the deadline")
}

func TestAutoSyncSchedulerSkipsWhenIntervalIsZero(t *testing.T) {
	var calls atomic.Int64
	dav := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer dav.Close()

	api, _, _ := newAutoSyncApi(t)
	if err := api.SetSyncConfig(appsync.DavConfig{
		Url: dav.URL, Username: "u", Password: "p", IntervalMinutes: 0,
	}); err != nil {
		t.Fatal(err)
	}
	stop := api.StartAutoSync(time.Millisecond * 30)
	defer stop()
	time.Sleep(300 * time.Millisecond)
	if calls.Load() != 0 {
		t.Fatalf("interval 0 must disable auto sync, got %d calls", calls.Load())
	}
}

func TestAutoSyncMarkerBacksOffAndSurvivesRestart(t *testing.T) {
	api, store, wsId := newAutoSyncApi(t)
	if err := api.SetSyncConfig(appsync.DavConfig{
		Url: "https://dav.invalid.test", Username: "u", Password: "p", IntervalMinutes: 5,
	}); err != nil {
		t.Fatal(err)
	}
	// 从未同步过：到期应立即触发
	if a, b := wsId, 5; a == "" || b == 0 {
		t.Fatal("unreachable")
	}
	if api.notDueAutoSync(wsId, 5) {
		t.Fatal("never-synced workspace must be due")
	}
	// 尝试后写 marker（成功或失败都写，防止失败时 30s 重试风暴）
	if err := api.markAutoSyncDone(wsId); err != nil {
		t.Fatal(err)
	}
	if !api.notDueAutoSync(wsId, 5) {
		t.Fatal("just-attempted run must not be due again immediately")
	}
	// marker 落在 setting 表：重启后（新 SyncApi 实例）读取同一份
	raw, err := store.GetSetting("sync.auto." + wsId)
	if err != nil || raw == "" {
		t.Fatalf("marker missing: raw=%q err=%v", raw, err)
	}
	fresh := NewSyncApi(store, httpengine.New(), nil)
	if !fresh.notDueAutoSync(wsId, 5) {
		t.Fatal("fresh SyncApi must read the persisted marker")
	}
}

func TestAutoSyncStopsAndDedupsStart(t *testing.T) {
	api, _, _ := newAutoSyncApi(t)
	if err := api.SetSyncConfig(appsync.DavConfig{
		Url: "https://dav.invalid.test", Username: "u", Password: "p", IntervalMinutes: 15,
	}); err != nil {
		t.Fatal(err)
	}
	stop := api.StartAutoSync(time.Hour)  // 大 tick：不会真正触发同步
	stop2 := api.StartAutoSync(time.Hour) // 幂等：返回同一停止函数语义
	stop()
	stop2()
	// 停止后可再次启动
	stop3 := api.StartAutoSync(time.Hour)
	stop3()
}
