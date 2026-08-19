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
	if err != nil {
		return ProxyConfig{}, false, fmt.Errorf("read Windows proxy enabled state: %w", err)
	}
	config := ProxyConfig{}
	if pacURL, _, pacErr := key.GetStringValue("AutoConfigURL"); pacErr == nil && strings.TrimSpace(pacURL) != "" {
		config.Warning = "PAC automatic proxy configuration is not supported (" + strings.TrimSpace(pacURL) + ")"
	}
	if autoDetect, _, detectErr := key.GetIntegerValue("AutoDetect"); detectErr == nil && autoDetect != 0 {
		config.Warning = appendProxyWarning(config.Warning, "WPAD automatic proxy discovery is not supported")
	}
	if enabled == 0 {
		return config, false, nil
	}
	server, _, err := key.GetStringValue("ProxyServer")
	if err != nil || strings.TrimSpace(server) == "" {
		return ProxyConfig{}, false, fmt.Errorf("Windows system proxy is enabled without a proxy server")
	}
	bypass, _, _ := key.GetStringValue("ProxyOverride")
	parsed := parseWindowsProxy(server, bypass)
	parsed.Warning = config.Warning
	if parsed.HTTPProxy == "" && parsed.HTTPSProxy == "" {
		return ProxyConfig{}, false, fmt.Errorf("Windows system proxy contains no supported HTTP, HTTPS, or SOCKS endpoint")
	}
	return parsed, true, nil
}
