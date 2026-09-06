package sync

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"apirequest/backend/model"
)

// GET 捕获 ETag；PUT 以 If-Match 条件写入；服务器并发变更时回 412 → 专属错误
func TestDavConditionalPut(t *testing.T) {
	var lastIfMatch []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("ETag", `"etag-1"`)
			io.WriteString(w, "snapshot")
		case http.MethodPut:
			if im := r.Header.Values("If-Match"); len(im) > 0 {
				lastIfMatch = im
			}
			if im := r.Header.Get("If-Match"); im == `"etag-1"` {
				w.WriteHeader(http.StatusCreated)
				return
			}
			w.WriteHeader(http.StatusPreconditionFailed)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	client, err := newDavClient(DavConfig{Url: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	data, exists, etag, err := client.Get(ctx, "ApiRequest/ws.json")
	if err != nil || !exists || string(data) != "snapshot" || etag != `"etag-1"` {
		t.Fatalf("Get = %q exists=%v etag=%q err=%v", data, exists, etag, err)
	}
	if err := client.Put(ctx, "ApiRequest/ws.json", []byte("snapshot"), etag); err != nil {
		t.Fatalf("conditional Put: %v", err)
	}
	if len(lastIfMatch) != 1 || lastIfMatch[0] != `"etag-1"` {
		t.Fatalf("If-Match = %v", lastIfMatch)
	}
	// 旧 etag → 412 → 专属错误（引擎据此重拉重合并）
	err = client.Put(ctx, "ApiRequest/ws.json", []byte("snapshot"), `"stale"`)
	if !errors.Is(err, ErrRemoteConcurrent) {
		t.Fatalf("stale etag err = %v, want ErrRemoteConcurrent", err)
	}
}

// 服务器不发 ETag（坚果云等兼容场景）：PUT 不带 If-Match，行为与旧版一致
func TestDavConditionalDegraded(t *testing.T) {
	var sawIfMatch bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			io.WriteString(w, "snapshot")
		case http.MethodPut:
			if r.Header.Get("If-Match") != "" {
				sawIfMatch = true
			}
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	client, _ := newDavClient(DavConfig{Url: srv.URL})
	ctx := context.Background()
	_, _, etag, err := client.Get(ctx, "ApiRequest/ws.json")
	if err != nil || etag != "" {
		t.Fatalf("Get etag = %q err=%v", etag, err)
	}
	if err := client.Put(ctx, "ApiRequest/ws.json", []byte("snapshot"), etag); err != nil {
		t.Fatalf("unconditional Put: %v", err)
	}
	if sawIfMatch {
		t.Fatal("must not send If-Match without a server ETag")
	}
}

// Get 404 语义保持：远端不存在 → exists=false
func TestDavGetMissingKeepsSemantics(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()
	client, _ := newDavClient(DavConfig{Url: srv.URL})
	_, exists, etag, err := client.Get(context.Background(), "ApiRequest/ws.json")
	if err != nil || exists || etag != "" {
		t.Fatalf("Get = exists=%v etag=%q err=%v", exists, etag, err)
	}
	_ = model.KindNetwork // 保持 model 导入（错误路径共用）
}

// 引擎级：PUT 412（远端被并发修改）→ 重拉重合并重推，本地拿到并发写者的实体
func TestSyncRetriesOnRemoteConcurrentChange(t *testing.T) {
	etagSeq := 0
	putCount := 0
	snapV1 := `{"schemaVersion":2,"workspaceName":"w","syncedAt":1,"nodes":[{"id":"n1","kind":"collection","name":"c","createdAt":1,"updatedAt":1}],"environments":[],"globals":[],"globalsRev":0}`
	snapV2 := `{"schemaVersion":2,"workspaceName":"w","syncedAt":2,"nodes":[` +
		`{"id":"n1","kind":"collection","name":"c","createdAt":1,"updatedAt":2},` +
		`{"id":"n2","kind":"collection","name":"from-concurrent","createdAt":2,"updatedAt":2}` +
		`],"environments":[],"globals":[],"globalsRev":0}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			etagSeq++
			t.Logf("GET #%d", etagSeq)
			w.Header().Set("ETag", "\"e"+itoa(etagSeq)+"\"")
			if etagSeq == 1 {
				io.WriteString(w, snapV1)
			} else {
				io.WriteString(w, snapV2)
			}
		case http.MethodPut:
			putCount++
			t.Logf("PUT #%d If-Match=%q", putCount, r.Header.Get("If-Match"))
			if r.Header.Get("If-Match") == `"e1"` {
				w.WriteHeader(http.StatusPreconditionFailed) // 模拟并发写者抢先
				return
			}
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	store, wsId := newDevice(t)
	rep, err := Sync(store, wsId, DavConfig{Url: srv.URL})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if putCount != 2 {
		t.Fatalf("putCount = %d, want 2 (first 412, then success)", putCount)
	}
	// 并发写者的实体已并入本地
	if _, err := store.GetNode(wsId, "n2"); err != nil {
		t.Fatalf("concurrent node missing locally: %v", err)
	}
	if rep.Pushed == 0 && rep.Pulled == 0 {
		t.Fatalf("report = %+v, want non-zero counters after re-merge", rep)
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var b []byte
	for v > 0 {
		b = append([]byte{byte('0' + v%10)}, b...)
		v /= 10
	}
	return string(b)
}
