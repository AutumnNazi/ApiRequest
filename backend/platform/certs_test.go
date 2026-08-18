package platform

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTestCertificatePair(t *testing.T, dir string) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "platform-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(dir, "client.pem")
	keyPath := filepath.Join(dir, "client-key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath
}

func TestLoadTLSConfigEmptyAndValidMaterial(t *testing.T) {
	config, err := LoadTLSConfig(CertificateSettings{})
	if err != nil || config != nil {
		t.Fatalf("empty TLS config = %+v, err = %v", config, err)
	}

	dir := t.TempDir()
	certPath, keyPath := writeTestCertificatePair(t, dir)
	config, err = LoadTLSConfig(CertificateSettings{
		CACertPath:     certPath,
		ClientCertPath: certPath,
		ClientKeyPath:  keyPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	if config.RootCAs == nil || len(config.Certificates) != 1 {
		t.Fatalf("loaded TLS config = %+v", config)
	}
}

func TestLoadTLSConfigRejectsInvalidMaterial(t *testing.T) {
	if _, err := LoadTLSConfig(CertificateSettings{ClientCertPath: "client.pem"}); err == nil {
		t.Fatal("client certificate without key was accepted")
	}
	dir := t.TempDir()
	badPEM := filepath.Join(dir, "bad.pem")
	if err := os.WriteFile(badPEM, []byte("not PEM"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTLSConfig(CertificateSettings{CACertPath: badPEM}); err == nil || !strings.Contains(err.Error(), "no valid certificates") {
		t.Fatalf("invalid CA error = %v", err)
	}
	if _, err := LoadTLSConfig(CertificateSettings{CACertPath: dir}); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("directory certificate error = %v", err)
	}
	oversized := filepath.Join(dir, "oversized.pem")
	file, err := os.Create(oversized)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaxCertificateMaterialSize + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTLSConfig(CertificateSettings{CACertPath: oversized}); err == nil || !strings.Contains(err.Error(), "4 MiB") {
		t.Fatalf("oversized certificate error = %v", err)
	}
}
