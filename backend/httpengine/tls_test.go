package httpengine

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// selfSignedPair 生成自签 CA + 服务端证书，返回 (caPEM 路径, tls.Certificate)
func selfSignedServer(t *testing.T, dir string) (string, tls.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-ca"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true,
		BasicConstraintsValid: true,
		IPAddresses:  nil,
		DNSNames:     []string{"localhost", "127.0.0.1"},
	}
	// httptest 用 IP 连接
	tmpl.IPAddresses = append(tmpl.IPAddresses, []byte{127, 0, 0, 1})

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	caPath := filepath.Join(dir, "ca.pem")
	os.WriteFile(caPath, certPEM, 0o600)

	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return caPath, pair
}

func TestCustomCA(t *testing.T) {
	dir := t.TempDir()
	caPath, serverCert := selfSignedServer(t, dir)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("trusted"))
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{serverCert}}
	srv.StartTLS()
	defer srv.Close()
	// httptest.StartTLS 会用自己的证书；换成我们的自签证书
	srv.TLS.Certificates = []tls.Certificate{serverCert}

	e := New()
	// 未信任自签 CA：应报 TLS 错
	req := testReq(srv.URL)
	if _, err := e.Send(context.Background(), req); err == nil {
		t.Fatal("untrusted CA should fail")
	}
	// 配置自定义 CA 后应成功
	if err := e.SetTLS(TLSSettings{CaCertPath: caPath}); err != nil {
		t.Fatalf("set tls: %v", err)
	}
	res, err := e.Send(context.Background(), req)
	if err != nil {
		t.Fatalf("send with custom CA: %v", err)
	}
	if res.Body.Text != "trusted" {
		t.Errorf("body = %q", res.Body.Text)
	}
	// 清空恢复默认：应再次失败
	if err := e.SetTLS(TLSSettings{}); err != nil {
		t.Fatalf("clear tls: %v", err)
	}
	if _, err := e.Send(context.Background(), req); err == nil {
		t.Error("after clearing CA should fail again")
	}
}

func TestTLSSettingsValidation(t *testing.T) {
	e := New()
	if err := e.SetTLS(TLSSettings{CaCertPath: "/nonexistent/ca.pem"}); err == nil {
		t.Error("missing CA file should error")
	}
	if err := e.SetTLS(TLSSettings{ClientCertPath: "/a.pem"}); err == nil {
		t.Error("cert without key should error")
	}
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.pem")
	os.WriteFile(bad, []byte("not a pem"), 0o600)
	if err := e.SetTLS(TLSSettings{CaCertPath: bad}); err == nil {
		t.Error("invalid PEM should error")
	}
}
