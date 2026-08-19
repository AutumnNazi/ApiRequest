package platform

import (
	"net/http"
	"strings"
	"testing"
)

func TestManualProxyFuncValidationAndResolution(t *testing.T) {
	for _, value := range []string{"", "proxy.example:8080", "ftp://proxy.example:21", "://bad"} {
		if _, err := ManualProxyFunc(value); err == nil {
			t.Errorf("invalid manual proxy %q was accepted", value)
		}
	}
	resolver, err := ManualProxyFunc("SOCKS5://proxy.example:1080")
	if err != nil {
		t.Fatal(err)
	}
	request, _ := http.NewRequest(http.MethodGet, "https://api.example.com", nil)
	proxyURL, err := resolver(request)
	if err != nil || proxyURL.String() != "socks5://proxy.example:1080" {
		t.Fatalf("manual proxy = %v, err = %v", proxyURL, err)
	}
}

func TestProxyConfigSelectsSchemeAndBypass(t *testing.T) {
	resolver := (ProxyConfig{
		HTTPProxy:              "http://http-proxy.example:8080",
		HTTPSProxy:             "http://https-proxy.example:8443",
		NoProxy:                ".internal.example",
		ExcludeSimpleHostnames: true,
	}).ProxyFunc()
	if resolver == nil {
		t.Fatal("configured proxy returned no resolver")
	}
	tests := []struct {
		target string
		want   string
	}{
		{target: "http://api.example.com", want: "http://http-proxy.example:8080"},
		{target: "https://api.example.com", want: "http://https-proxy.example:8443"},
		{target: "https://service.internal.example", want: ""},
		{target: "http://intranet", want: ""},
	}
	for _, test := range tests {
		request, _ := http.NewRequest(http.MethodGet, test.target, nil)
		proxyURL, err := resolver(request)
		if err != nil {
			t.Fatalf("resolve %s: %v", test.target, err)
		}
		got := ""
		if proxyURL != nil {
			got = proxyURL.String()
		}
		if got != test.want {
			t.Errorf("proxy for %s = %q, want %q", test.target, got, test.want)
		}
	}
	if _, err := resolver(nil); err == nil {
		t.Fatal("nil proxy request was accepted")
	}
}

func TestParseWindowsProxy(t *testing.T) {
	single := parseWindowsProxy("HTTP://proxy.example:8080", "localhost;<local>;*.internal.example")
	if single.HTTPProxy != "http://proxy.example:8080" || single.HTTPSProxy != single.HTTPProxy {
		t.Fatalf("single proxy = %+v", single)
	}
	if single.NoProxy != "localhost,.internal.example" || !single.ExcludeSimpleHostnames {
		t.Fatalf("single proxy bypass = %+v", single)
	}

	perScheme := parseWindowsProxy(
		"http=http-proxy.example:80;https=secure-proxy.example:443;socks=socks.example:1080;ftp=ignored.example:21",
		"",
	)
	if perScheme.HTTPProxy != "http://http-proxy.example:80" || perScheme.HTTPSProxy != "http://secure-proxy.example:443" {
		t.Fatalf("per-scheme proxy = %+v", perScheme)
	}

	socks := parseWindowsProxy("socks=socks.example:1080", "")
	if socks.HTTPProxy != "socks5://socks.example:1080" || socks.HTTPSProxy != socks.HTTPProxy {
		t.Fatalf("SOCKS proxy = %+v", socks)
	}
}

func TestParseDarwinProxy(t *testing.T) {
	output := `<dictionary> {
  ExceptionsList : <array> {
    0 : *.internal.example
    1 : localhost
  }
  ExcludeSimpleHostnames : 1
  HTTPEnable : 1
  HTTPPort : 8080
  HTTPProxy : http-proxy.example
  HTTPSEnable : 1
  HTTPSPort : 8443
  HTTPSProxy : https-proxy.example
}`
	config, found := parseDarwinProxy(output)
	if !found || config.Source != "darwin" {
		t.Fatalf("macOS proxy not found: %+v", config)
	}
	if config.HTTPProxy != "http://http-proxy.example:8080" || config.HTTPSProxy != "http://https-proxy.example:8443" {
		t.Fatalf("macOS proxy endpoints = %+v", config)
	}
	if config.NoProxy != ".internal.example,localhost" || !config.ExcludeSimpleHostnames {
		t.Fatalf("macOS proxy bypass = %+v", config)
	}

	socksOutput := strings.NewReplacer(
		"HTTPEnable : 1", "HTTPEnable : 0",
		"HTTPSEnable : 1", "HTTPSEnable : 0",
	).Replace(output) + "\nSOCKSEnable : 1\nSOCKSProxy : socks.example\nSOCKSPort : 1080"
	socks, found := parseDarwinProxy(socksOutput)
	if !found || socks.HTTPProxy != "socks5://socks.example:1080" || socks.HTTPSProxy != socks.HTTPProxy {
		t.Fatalf("macOS SOCKS fallback = %+v", socks)
	}

	ipv6, found := parseDarwinProxy("HTTPEnable : 1\nHTTPProxy : 2001:db8::1\nHTTPPort : 8080")
	if !found || ipv6.HTTPProxy != "http://[2001:db8::1]:8080" {
		t.Fatalf("macOS IPv6 proxy = %+v", ipv6)
	}

	automatic, found := parseDarwinProxy("ProxyAutoConfigEnable : 1\nProxyAutoConfigURLString : https://proxy.example/proxy.pac")
	if found || !strings.Contains(automatic.Warning, "PAC") {
		t.Fatalf("macOS automatic proxy diagnostic = %+v, found = %v", automatic, found)
	}
}
