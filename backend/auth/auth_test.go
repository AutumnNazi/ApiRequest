package auth

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"apirequest/backend/model"
)

func newReq(t *testing.T, method, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestBasic(t *testing.T) {
	req := newReq(t, "GET", "https://a.io/")
	err := Apply(req, model.Auth{Type: "basic", Params: map[string]string{
		"username": "user", "password": "p@ss",
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:p@ss"))
	if got := req.Header.Get("Authorization"); got != want {
		t.Errorf("auth = %q, want %q", got, want)
	}
}

func TestBearer(t *testing.T) {
	req := newReq(t, "GET", "https://a.io/")
	Apply(req, model.Auth{Type: "bearer", Params: map[string]string{"token": "tok123"}})
	if got := req.Header.Get("Authorization"); got != "Bearer tok123" {
		t.Errorf("auth = %q", got)
	}
}

func TestApiKeyHeaderAndQuery(t *testing.T) {
	req := newReq(t, "GET", "https://a.io/p")
	Apply(req, model.Auth{Type: "apikey", Params: map[string]string{
		"key": "X-Api-Key", "value": "k1", "in": "header",
	}})
	if got := req.Header.Get("X-Api-Key"); got != "k1" {
		t.Errorf("header = %q", got)
	}

	req2 := newReq(t, "GET", "https://a.io/p")
	Apply(req2, model.Auth{Type: "apikey", Params: map[string]string{
		"key": "api_key", "value": "k2", "in": "query",
	}})
	if got := req2.URL.Query().Get("api_key"); got != "k2" {
		t.Errorf("query = %q", got)
	}
}

func TestNoneAndUnknown(t *testing.T) {
	req := newReq(t, "GET", "https://a.io/")
	if err := Apply(req, model.Auth{Type: "none"}); err != nil {
		t.Errorf("none: %v", err)
	}
	if err := Apply(req, model.Auth{Type: "made-up"}); err == nil {
		t.Error("unknown type should error")
	}
}

func TestDigestChallenge(t *testing.T) {
	p, _ := Get("digest")
	tp := p.(TwoPhaseProvider)

	req := newReq(t, "GET", "https://a.io/dir/index.html")
	// RFC 2617 示例参数
	challenge := `Digest realm="testrealm@host.com", qop="auth,auth-int", nonce="dcd98b7102dd2f0e8b11d0f600bfb0c093", opaque="5ccc069c403ebaf9f0171e9517f40e41"`
	handled, err := tp.OnChallenge(req, challenge, map[string]string{
		"username": "Mufasa", "password": "Circle Of Life",
	})
	if err != nil || !handled {
		t.Fatalf("handled=%v err=%v", handled, err)
	}
	got := req.Header.Get("Authorization")
	for _, part := range []string{
		`username="Mufasa"`, `realm="testrealm@host.com"`,
		`uri="/dir/index.html"`, `qop=auth`, `nc=00000001`,
		`opaque="5ccc069c403ebaf9f0171e9517f40e41"`,
	} {
		if !strings.Contains(got, part) {
			t.Errorf("missing %s in %s", part, got)
		}
	}
	if !regexp.MustCompile(`response="[0-9a-f]{32}"`).MatchString(got) {
		t.Errorf("bad response hash in %s", got)
	}
}

func TestDigestEndToEnd(t *testing.T) {
	// 模拟 Digest 服务器：无凭证 401，有 Authorization 则 200
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="r", nonce="n1", qop="auth"`)
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	p, _ := Get("digest")
	tp := p.(TwoPhaseProvider)
	req := newReq(t, "GET", srv.URL+"/x")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("first = %d", resp.StatusCode)
	}
	retry := newReq(t, "GET", srv.URL+"/x")
	handled, err := tp.OnChallenge(retry, resp.Header.Get("WWW-Authenticate"),
		map[string]string{"username": "u", "password": "p"})
	if !handled || err != nil {
		t.Fatalf("challenge: %v %v", handled, err)
	}
	resp2, _ := http.DefaultClient.Do(retry)
	resp2.Body.Close()
	if resp2.StatusCode != 200 {
		t.Errorf("retry = %d", resp2.StatusCode)
	}
}

func TestOAuth1Signature(t *testing.T) {
	req := newReq(t, "GET", "https://api.example.com/resource?b=2&a=1")
	err := Apply(req, model.Auth{Type: "oauth1", Params: map[string]string{
		"consumerKey": "ck", "consumerSecret": "cs",
		"token": "tk", "tokenSecret": "ts",
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := req.Header.Get("Authorization")
	if !strings.HasPrefix(got, "OAuth ") {
		t.Fatalf("auth = %q", got)
	}
	for _, part := range []string{
		`oauth_consumer_key="ck"`, `oauth_token="tk"`,
		`oauth_signature_method="HMAC-SHA1"`, `oauth_signature=`,
		`oauth_nonce=`, `oauth_timestamp=`,
	} {
		if !strings.Contains(got, part) {
			t.Errorf("missing %s", part)
		}
	}
}

func TestAWSSigV4(t *testing.T) {
	req := newReq(t, "GET", "https://examplebucket.s3.amazonaws.com/test.txt")
	err := Apply(req, model.Auth{Type: "awsv4", Params: map[string]string{
		"accessKey": "AKIDEXAMPLE", "secretKey": "secret",
		"region": "us-east-1", "service": "s3",
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := req.Header.Get("Authorization")
	if !strings.HasPrefix(got, "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/") {
		t.Errorf("auth = %q", got)
	}
	if !strings.Contains(got, "/us-east-1/s3/aws4_request") ||
		!strings.Contains(got, "SignedHeaders=") ||
		!regexp.MustCompile(`Signature=[0-9a-f]{64}$`).MatchString(got) {
		t.Errorf("auth format = %q", got)
	}
	if req.Header.Get("X-Amz-Date") == "" || req.Header.Get("X-Amz-Content-Sha256") == "" {
		t.Error("missing x-amz headers")
	}
}

func TestAWSSigV4HashesBinaryBodyWithoutConsumingIt(t *testing.T) {
	payload := []byte{0x00, 0xff, 0x01, 'A', '\n'}
	req, err := http.NewRequest(http.MethodPost, "https://examplebucket.s3.amazonaws.com/object", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if err := Apply(req, model.Auth{Type: "awsv4", Params: map[string]string{
		"accessKey": "AKIDEXAMPLE", "secretKey": "secret", "region": "us-east-1", "service": "s3",
	}}); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(payload)
	if got, want := req.Header.Get("X-Amz-Content-Sha256"), hex.EncodeToString(hash[:]); got != want {
		t.Fatalf("payload hash = %q, want %q", got, want)
	}
	got, err := io.ReadAll(req.Body)
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("signed body changed: %x, %v", got, err)
	}
}

type endlessSigV4Reader struct{ read int }

func (r *endlessSigV4Reader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	r.read += len(p)
	return len(p), nil
}

func TestAWSSigV4BoundsNonReplayableBodyBuffer(t *testing.T) {
	reader := &endlessSigV4Reader{}
	req, err := http.NewRequest(http.MethodPost, "https://examplebucket.s3.amazonaws.com/object", reader)
	if err != nil {
		t.Fatal(err)
	}
	err = Apply(req, model.Auth{Type: "awsv4", Params: map[string]string{
		"accessKey": "AKIDEXAMPLE", "secretKey": "secret", "region": "us-east-1", "service": "s3",
	}})
	if err == nil || !strings.Contains(err.Error(), "non-replayable") {
		t.Fatalf("oversized one-shot body error = %v", err)
	}
	if reader.read != maxBufferedSigV4Body+1 {
		t.Fatalf("read %d bytes, want bounded read of %d", reader.read, maxBufferedSigV4Body+1)
	}
}
