package updater

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.3", "1.2.3", 0},
		{"1.2.4", "1.2.3", 1},
		{"1.10.0", "1.9.9", 1}, // 数值比较，不是字典序
		{"2.0.0", "10.0.0", -1},
		{"1.2", "1.2.0", 0},         // 缺段按 0
		{"1.2.3-beta", "1.2.3", -1}, // 预发布 < 正式
		{"v1.2.3", "1.2.3", 0},      // v 前缀容忍
		{"dev", "1.0.0", 0},         // 不可解析：视为相同（由调用方决定语义）
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// 测试签名服务器：manifest + .sig
func startManifestServer(t *testing.T, priv ed25519.PrivateKey, manifest []byte) *httptest.Server {
	t.Helper()
	sig := ed25519.Sign(priv, manifest)
	mux := http.NewServeMux()
	mux.HandleFunc("/manifest.json", func(w http.ResponseWriter, r *http.Request) {
		w.Write(manifest)
	})
	mux.HandleFunc("/manifest.json.sig", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(base64.StdEncoding.EncodeToString(sig)))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func mustManifest(t *testing.T, body string) []byte {
	t.Helper()
	return []byte(strings.TrimSpace(body))
}

func TestCheckUpdateAvailable(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	manifest := mustManifest(t, `{"version":"1.2.0","notesUrl":"https://x.test/notes","platforms":{"windows-amd64":{"url":"https://x.test/setup.msi","sha256":"abc"}}}`)
	srv := startManifestServer(t, priv, manifest)

	res, err := Check(context.Background(), Config{
		ManifestURL:    srv.URL + "/manifest.json",
		PublicKey:      pub,
		CurrentVersion: "1.1.0",
		Platform:       "windows-amd64",
	})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if res.Status != StatusAvailable {
		t.Fatalf("status = %s (%s)", res.Status, res.Detail)
	}
	if res.LatestVersion != "1.2.0" || res.DownloadURL != "https://x.test/setup.msi" {
		t.Fatalf("result = %+v", res)
	}
}

func TestCheckUpToDate(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	manifest := mustManifest(t, `{"version":"1.2.0","platforms":{}}`)
	srv := startManifestServer(t, priv, manifest)
	res, err := Check(context.Background(), Config{
		ManifestURL: srv.URL + "/manifest.json", PublicKey: pub, CurrentVersion: "1.2.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusUpToDate {
		t.Fatalf("status = %s", res.Status)
	}
}

// 版本低于 floor：不允许静默更新，提示手动安装
func TestCheckBelowFloor(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	manifest := mustManifest(t, `{"version":"3.0.0","floor":"2.5.0","platforms":{}}`)
	srv := startManifestServer(t, priv, manifest)
	res, err := Check(context.Background(), Config{
		ManifestURL: srv.URL + "/manifest.json", PublicKey: pub, CurrentVersion: "1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusManualRequired {
		t.Fatalf("status = %s", res.Status)
	}
}

// 签名不匹配：明确失败，绝不采信内容
func TestCheckBadSignature(t *testing.T) {
	pub, otherPriv, _ := ed25519.GenerateKey(rand.Reader)
	_, attackerPriv, _ := ed25519.GenerateKey(rand.Reader)
	_ = otherPriv
	manifest := mustManifest(t, `{"version":"9.9.9","platforms":{}}`)
	sig := ed25519.Sign(attackerPriv, manifest)
	mux := http.NewServeMux()
	mux.HandleFunc("/manifest.json", func(w http.ResponseWriter, r *http.Request) { w.Write(manifest) })
	mux.HandleFunc("/manifest.json.sig", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(base64.StdEncoding.EncodeToString(sig)))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	res, err := Check(context.Background(), Config{
		ManifestURL: srv.URL + "/manifest.json", PublicKey: pub, CurrentVersion: "1.0.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusVerificationFailed {
		t.Fatalf("status = %s, want verification-failed", res.Status)
	}
}

// 未知平台：available 但无下载地址（详情说明）
func TestCheckUnknownPlatform(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	manifest := mustManifest(t, `{"version":"1.2.0","platforms":{}}`)
	srv := startManifestServer(t, priv, manifest)
	res, err := Check(context.Background(), Config{
		ManifestURL: srv.URL + "/manifest.json", PublicKey: pub, CurrentVersion: "1.0.0",
		Platform: "plan9-amd64",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != StatusAvailable || res.DownloadURL != "" {
		t.Fatalf("res = %+v", res)
	}
}
