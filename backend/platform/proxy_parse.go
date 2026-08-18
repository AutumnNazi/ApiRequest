package platform

import (
	"net"
	"strconv"
	"strings"
)

// The native readers live behind build tags, while these parsers stay common
// so every CI host can regression-test Windows and macOS proxy formats.
func parseWindowsProxy(server, bypass string) ProxyConfig {
	config := ProxyConfig{Source: "windows"}
	if !strings.Contains(server, "=") {
		proxyURL := normalizeProxyURL(server, "http")
		config.HTTPProxy = proxyURL
		config.HTTPSProxy = proxyURL
	} else {
		for _, entry := range strings.Split(server, ";") {
			key, value, found := strings.Cut(entry, "=")
			if !found {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "http":
				config.HTTPProxy = normalizeProxyURL(value, "http")
			case "https":
				config.HTTPSProxy = normalizeProxyURL(value, "http")
			case "socks", "socks5":
				proxyURL := normalizeProxyURL(value, "socks5")
				if config.HTTPProxy == "" {
					config.HTTPProxy = proxyURL
				}
				if config.HTTPSProxy == "" {
					config.HTTPSProxy = proxyURL
				}
			}
		}
	}
	config.NoProxy, config.ExcludeSimpleHostnames = normalizeNoProxy(bypass)
	return config
}

func parseDarwinProxy(output string) (ProxyConfig, bool) {
	values := map[string]string{}
	exceptions := []string{}
	inExceptions := false
	for _, rawLine := range strings.Split(output, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "ExceptionsList") {
			inExceptions = true
			continue
		}
		if inExceptions {
			if line == "}" {
				inExceptions = false
				continue
			}
			if _, value, found := strings.Cut(line, ":"); found {
				exceptions = append(exceptions, strings.TrimSpace(value))
			}
			continue
		}
		if key, value, found := strings.Cut(line, ":"); found {
			values[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}

	config := ProxyConfig{Source: "darwin"}
	if values["HTTPEnable"] == "1" {
		config.HTTPProxy = hostPortProxy(values["HTTPProxy"], values["HTTPPort"], "http")
	}
	if values["HTTPSEnable"] == "1" {
		config.HTTPSProxy = hostPortProxy(values["HTTPSProxy"], values["HTTPSPort"], "http")
	}
	if config.HTTPProxy == "" && config.HTTPSProxy == "" && values["SOCKSEnable"] == "1" {
		proxyURL := hostPortProxy(values["SOCKSProxy"], values["SOCKSPort"], "socks5")
		config.HTTPProxy = proxyURL
		config.HTTPSProxy = proxyURL
	}
	config.NoProxy, config.ExcludeSimpleHostnames = normalizeNoProxy(strings.Join(exceptions, ","))
	if values["ExcludeSimpleHostnames"] == "1" {
		config.ExcludeSimpleHostnames = true
	}
	return config, config.HTTPProxy != "" || config.HTTPSProxy != ""
}

func hostPortProxy(host, port, scheme string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if parsedPort, err := strconv.Atoi(strings.TrimSpace(port)); err == nil && parsedPort > 0 {
		if net.ParseIP(host) != nil {
			host = net.JoinHostPort(host, strconv.Itoa(parsedPort))
		} else {
			host += ":" + strconv.Itoa(parsedPort)
		}
	}
	return normalizeProxyURL(host, scheme)
}
