//go:build darwin

package platform

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

func systemProxyConfig() (ProxyConfig, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "/usr/sbin/scutil", "--proxy").Output()
	if err != nil {
		return ProxyConfig{}, false, fmt.Errorf("read macOS system proxy: %w", err)
	}
	config, found := parseDarwinProxy(string(output))
	return config, found, nil
}
