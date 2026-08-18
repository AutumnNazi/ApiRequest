package platform

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"os"
)

// MaxCertificateMaterialSize bounds each user-selected certificate or key.
const MaxCertificateMaterialSize int64 = 4 << 20

// CertificateSettings describes file-based additions to the operating
// system's trust store and an optional mTLS client identity.
type CertificateSettings struct {
	CACertPath     string
	ClientCertPath string
	ClientKeyPath  string
}

// LoadTLSConfig loads certificate material on top of the OS trust store. An
// empty settings value returns nil so net/http uses its native defaults.
func LoadTLSConfig(settings CertificateSettings) (*tls.Config, error) {
	if settings.CACertPath == "" && settings.ClientCertPath == "" && settings.ClientKeyPath == "" {
		return nil, nil
	}
	if (settings.ClientCertPath == "") != (settings.ClientKeyPath == "") {
		return nil, errors.New("client cert and key must both be set")
	}

	config := &tls.Config{}
	if settings.CACertPath != "" {
		pemData, err := readCertificateFile(settings.CACertPath)
		if err != nil {
			return nil, fmt.Errorf("read CA cert: %w", err)
		}
		roots, err := x509.SystemCertPool()
		if err != nil || roots == nil {
			roots = x509.NewCertPool()
		}
		if !roots.AppendCertsFromPEM(pemData) {
			return nil, fmt.Errorf("no valid certificates in %s", settings.CACertPath)
		}
		config.RootCAs = roots
	}

	if settings.ClientCertPath != "" {
		certPEM, err := readCertificateFile(settings.ClientCertPath)
		if err != nil {
			return nil, fmt.Errorf("read client cert: %w", err)
		}
		keyPEM, err := readCertificateFile(settings.ClientKeyPath)
		if err != nil {
			return nil, fmt.Errorf("read client key: %w", err)
		}
		certificate, err := tls.X509KeyPair(certPEM, keyPEM)
		if err != nil {
			return nil, fmt.Errorf("load client cert: %w", err)
		}
		config.Certificates = []tls.Certificate{certificate}
	}
	return config, nil
}

func readCertificateFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("path is not a regular file")
	}
	if info.Size() > MaxCertificateMaterialSize {
		return nil, fmt.Errorf("file exceeds %d MiB limit", MaxCertificateMaterialSize>>20)
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxCertificateMaterialSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > MaxCertificateMaterialSize {
		return nil, fmt.Errorf("file exceeds %d MiB limit", MaxCertificateMaterialSize>>20)
	}
	return data, nil
}
