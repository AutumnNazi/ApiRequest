//go:build !windows && !darwin

package platform

func systemProxyConfig() (ProxyConfig, bool, error) {
	return ProxyConfig{}, false, nil
}
