// Package platform 收敛 OS 差异（docs/overview.md §4）。
// Phase 1 仅需 paths；secrets/proxy/certs/open 随后续阶段落地。
package platform

import (
	"os"
	"path/filepath"
)

const appDirName = "com.apirequest.app"

// DataDir 返回应用数据目录（不存在则创建）。
// Windows: %APPDATA%\com.apirequest.app  macOS: ~/Library/Application Support/com.apirequest.app
func DataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, appDirName)
	for _, sub := range []string{"", "blobs", "logs"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return "", err
		}
	}
	return dir, nil
}
