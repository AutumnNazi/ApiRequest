//go:build windows

package platform

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func systemProxyConfig() (ProxyConfig, bool, error) {
	key, err := registry.OpenKey(
		registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Internet Settings`,
		registry.QUERY_VALUE,
	)
	if err != nil {
		return ProxyConfig{}, false, fmt.Errorf("read Windows system proxy: %w", err)
	}
	defer key.Close()

	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return ProxyConfig{}, false, nil
	}
	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil || strings.TrimSpace(server) == "" {
		return ProxyConfig{}, false, fmt.Errorf("Windows system proxy is enabled without a proxy server")
	}
	bypass, _, _ := key.GetStringValue("ProxyOverride")
	config := parseWindowsProxy(server, bypass)
	if config.HTTPProxy == "" && config.HTTPSProxy == "" {
		return ProxyConfig{}, false, fmt.Errorf("Windows system proxy contains no supported HTTP, HTTPS, or SOCKS endpoint")
	}
	return config, true, nil
}
