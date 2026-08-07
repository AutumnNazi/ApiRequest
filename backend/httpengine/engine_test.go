package httpengine

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

type errorAfterReader struct {
	reader io.Reader
	err    error
	failed bool
}

func (r *errorAfterReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	if n > 0 || (err != nil && !errors.Is(err, io.EOF)) {
		return n, err
	}
	if !r.failed {
		r.failed = true
		return 0, r.err
	}
	return 0, io.EOF
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

func TestBuildRequestDoesNotDuplicateExactURLParam(t *testing.T) {
	req, err := New().buildRequest(context.Background(), model.HttpRequest{
		Method: "GET",
		Url:    "https://example.test/items?tag=one",
		Params: []model.KV{
			{Key: "tag", Value: "one", Enabled: true},
			{Key: "tag", Value: "two", Enabled: true},
		},
		Settings: model.DefaultSettings(),
	})
	if err != nil {
		t.Fatal(err)
	}
	values := req.URL.Query()["tag"]
	if len(values) != 2 || values[0] != "one" || values[1] != "two" {
		t.Fatalf("tag values = %v, want [one two]", values)
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
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != res.Body.BlobRef {
		t.Fatalf("unexpected blob directory contents: %+v", entries)
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

func TestSmallBinaryBodyUsesBase64(t *testing.T) {
	imageBytes := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0xff}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(imageBytes)
	}))
	defer srv.Close()
	result, err := New().Send(context.Background(), testReq(srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Body.Inline || result.Body.Encoding != "base64" {
		t.Fatalf("body = %+v", result.Body)
	}
	decoded, err := base64.StdEncoding.DecodeString(result.Body.Text)
	if err != nil || !bytes.Equal(decoded, imageBytes) {
		t.Fatalf("decoded = %v, err = %v", decoded, err)
	}
}

func TestSendWithProgressReportsTTFBAndDownloadedBytes(t *testing.T) {
	body := bytes.Repeat([]byte("progress-data"), 20_000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	var progress []Progress
	result, err := New().SendWithProgress(context.Background(), testReq(srv.URL), func(item Progress) {
		progress = append(progress, item)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(progress) < 2 || progress[0].Phase != "ttfb" {
		t.Fatalf("progress = %+v", progress)
	}
	last := progress[len(progress)-1]
	if last.Phase != "downloading" || last.BytesReceived != int64(len(body)) || last.TotalBytes != int64(len(body)) {
		t.Fatalf("last progress = %+v, result size = %d", last, result.SizeBytes)
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

func TestMultipartBodyStreamsAndCanBeReplayed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "payload.bin")
	if err := os.WriteFile(path, []byte("streamed-file-content"), 0o600); err != nil {
		t.Fatal(err)
	}

	req, err := New().buildRequest(context.Background(), model.HttpRequest{
		Method: "POST",
		Url:    "https://example.test/upload",
		Body: model.Body{Kind: "formdata", Items: []model.FormItem{
			{Key: "label", Type: "text", Value: "demo", Enabled: true},
			{Key: "asset", Type: "file", Path: path, Enabled: true},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer req.Body.Close()
	if _, ok := req.Body.(*io.PipeReader); !ok {
		t.Fatalf("multipart body type = %T, want streaming pipe", req.Body)
	}
	if req.GetBody == nil {
		t.Fatal("multipart body is not replayable")
	}

	readReplay := func() []byte {
		t.Helper()
		body, err := req.GetBody()
		if err != nil {
			t.Fatal(err)
		}
		defer body.Close()
		data, err := io.ReadAll(body)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	first, second := readReplay(), readReplay()
	if !bytes.Equal(first, second) {
		t.Fatal("multipart replay changed the encoded body")
	}

	mediaType, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		t.Fatalf("content type = %q, params = %v, err = %v", mediaType, params, err)
	}
	reader := multipart.NewReader(bytes.NewReader(first), params["boundary"])
	values := map[string]string{}
	filenames := map[string]string{}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(part)
		part.Close()
		if err != nil {
			t.Fatal(err)
		}
		values[part.FormName()] = string(data)
		filenames[part.FormName()] = part.FileName()
	}
	if values["label"] != "demo" || values["asset"] != "streamed-file-content" {
		t.Fatalf("multipart values = %#v", values)
	}
	if filenames["asset"] != "payload.bin" {
		t.Fatalf("multipart filename = %q", filenames["asset"])
	}
}

func TestFileBodiesRejectDirectories(t *testing.T) {
	dir := t.TempDir()
	for _, body := range []model.Body{
		{Kind: "binary", Path: dir},
		{Kind: "formdata", Items: []model.FormItem{{Key: "asset", Type: "file", Path: dir, Enabled: true}}},
	} {
		if _, _, err := buildBody(body); err == nil {
			t.Fatalf("directory accepted for %s body", body.Kind)
		}
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

func TestLargeBodyReadFailureDoesNotCommitBlob(t *testing.T) {
	injected := errors.New("injected read failure")
	reader := &errorAfterReader{
		reader: strings.NewReader(strings.Repeat("z", inlineBodyLimit+1000)),
		err:    injected,
	}
	dir := t.TempDir()
	e := New()
	e.SetBlobsDir(dir)

	_, _, blobRef, err := e.readBodyWithBlob(reader)
	if !errors.Is(err, injected) {
		t.Fatalf("read error = %v, want %v", err, injected)
	}
	if blobRef != "" {
		t.Fatalf("blob ref = %q after failed read", blobRef)
	}
	entries, readDirErr := os.ReadDir(dir)
	if readDirErr != nil {
		t.Fatal(readDirErr)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary blob was not cleaned up: %+v", entries)
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

func TestSetTLSConcurrentBuildClient(t *testing.T) {
	e := New()
	settings := model.DefaultSettings()
	settings.VerifyTLS = false

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for range 250 {
				if err := e.SetTLS(TLSSettings{}); err != nil {
					t.Errorf("set TLS: %v", err)
					return
				}
			}
		}()
		go func() {
			defer wg.Done()
			for range 250 {
				client := e.buildClient(settings)
				if client.Transport == nil {
					t.Error("client transport is nil")
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestHTTPClientUsesCurrentTLSSettings(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()

	engine := New()
	client := engine.NewHTTPClient(time.Second)
	if response, err := client.Get(server.URL); err == nil {
		response.Body.Close()
		t.Fatal("client trusted the test certificate before custom CA was configured")
	}

	caPath := filepath.Join(t.TempDir(), "ca.pem")
	certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	if err := os.WriteFile(caPath, certificate, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := engine.SetTLS(TLSSettings{CaCertPath: caPath}); err != nil {
		t.Fatal(err)
	}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("existing shared client did not observe updated TLS settings: %v", err)
	}
	response.Body.Close()
}

func TestSetTLSRejectsOversizedMaterial(t *testing.T) {
	path := filepath.Join(t.TempDir(), "oversized-ca.pem")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxTLSMaterialSize + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	err = New().SetTLS(TLSSettings{CaCertPath: path})
	if err == nil || !strings.Contains(err.Error(), "4 MiB") {
		t.Fatalf("oversized TLS material error = %v", err)
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
