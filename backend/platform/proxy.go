package platform

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/http/httpproxy"
)

// ProxyConfig is the normalized system-proxy snapshot used by net/http.
type ProxyConfig struct {
	Source                 string
	HTTPProxy              string
	HTTPSProxy             string
	NoProxy                string
	ExcludeSimpleHostnames bool
	Warning                string
}

// DetectSystemProxy prefers the native desktop proxy configuration and falls
// back to the process environment when native discovery is unavailable or no
// explicit native proxy is configured.
func DetectSystemProxy() ProxyConfig {
	config, found, err := systemProxyConfig()
	if found {
		if err != nil {
			config.Warning = appendProxyWarning(config.Warning, err.Error())
		}
		return config
	}

	nativeWarning := config.Warning
	if err != nil {
		nativeWarning = appendProxyWarning(nativeWarning, err.Error())
	}
	environment := httpproxy.FromEnvironment()
	config = ProxyConfig{
		Source:     "environment",
		HTTPProxy:  environment.HTTPProxy,
		HTTPSProxy: environment.HTTPSProxy,
		NoProxy:    environment.NoProxy,
	}
	if config.HTTPProxy == "" && config.HTTPSProxy == "" {
		config.Source = "direct"
	}
	config.Warning = nativeWarning
	return config
}

// ProxyFunc builds a request resolver from the normalized snapshot.
func (config ProxyConfig) ProxyFunc() func(*http.Request) (*url.URL, error) {
	if config.HTTPProxy == "" && config.HTTPSProxy == "" {
		return nil
	}
	resolver := (&httpproxy.Config{
		HTTPProxy:  config.HTTPProxy,
		HTTPSProxy: config.HTTPSProxy,
		NoProxy:    config.NoProxy,
	}).ProxyFunc()
	return func(request *http.Request) (*url.URL, error) {
		if request == nil || request.URL == nil {
			return nil, errors.New("proxy resolver requires a request URL")
		}
		hostname := request.URL.Hostname()
		if config.ExcludeSimpleHostnames && hostname != "" && !strings.Contains(hostname, ".") {
			return nil, nil
		}
		return resolver(request.URL)
	}
}

// SystemProxyFunc returns a resolver for the current native/environment
// snapshot. A nil result means direct connections.
func SystemProxyFunc() func(*http.Request) (*url.URL, error) {
	return DetectSystemProxy().ProxyFunc()
}

// ManualProxyFunc validates and builds a single-proxy resolver.
func ManualProxyFunc(rawURL string) (func(*http.Request) (*url.URL, error), error) {
	proxyURL, err := parseManualProxyURL(rawURL)
	if err != nil {
		return nil, err
	}
	return http.ProxyURL(proxyURL), nil
}

func parseManualProxyURL(rawURL string) (*url.URL, error) {
	proxyURL, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || proxyURL.Host == "" {
		return nil, errors.New("proxy URL must include a scheme and host")
	}
	proxyURL.Scheme = strings.ToLower(proxyURL.Scheme)
	switch proxyURL.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return nil, errors.New("proxy URL scheme must be http, https, socks5, or socks5h")
	}
	return proxyURL, nil
}

// SplitProxyURLCredentials removes userinfo from a proxy URL so credentials
// can be persisted separately in the Secret Vault.
func SplitProxyURLCredentials(rawURL string) (cleanURL, username, password string, passwordSet bool, err error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return "", "", "", false, errors.New("invalid proxy URL")
	}
	if parsed.User != nil {
		username = parsed.User.Username()
		password, passwordSet = parsed.User.Password()
		parsed.User = nil
	}
	return parsed.String(), username, password, passwordSet, nil
}

// ProxyURLWithCredentials validates a credential-free endpoint and adds the
// runtime-only credentials resolved from the Secret Vault.
func ProxyURLWithCredentials(rawURL, username, password string, passwordSet bool) (string, error) {
	parsed, err := parseManualProxyURL(rawURL)
	if err != nil {
		return "", err
	}
	if parsed.User != nil {
		return "", errors.New("proxy endpoint must not contain embedded credentials")
	}
	if passwordSet && username == "" {
		return "", errors.New("proxy username is required when a password is set")
	}
	if username != "" {
		if passwordSet {
			parsed.User = url.UserPassword(username, password)
		} else {
			parsed.User = url.User(username)
		}
	}
	return parsed.String(), nil
}

func appendProxyWarning(current, next string) string {
	if current == "" {
		return next
	}
	if next == "" || strings.Contains(current, next) {
		return current
	}
	return current + "; " + next
}

func normalizeProxyURL(value, defaultScheme string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return value
	}
	if !strings.Contains(value, "://") {
		value = defaultScheme + "://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return value
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	return parsed.String()
}

func normalizeNoProxy(value string) (string, bool) {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ';' || r == ',' })
	normalized := make([]string, 0, len(parts))
	excludeSimple := false
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.EqualFold(part, "<local>") {
			excludeSimple = true
			continue
		}
		if strings.HasPrefix(part, "*.") {
			part = strings.TrimPrefix(part, "*")
		}
		normalized = append(normalized, part)
	}
	return strings.Join(normalized, ","), excludeSimple
}
