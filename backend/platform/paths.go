// Package platform 收敛 OS 差异（docs/overview.md §4）。
package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const appDirName = "com.apirequest.app"

// Paths contains every application-owned filesystem root. Keeping these paths
// together prevents callers from rebuilding platform-specific paths by hand.
type Paths struct {
	Data   string
	Blobs  string
	Logs   string
	Protos string
}

// ResolvePaths resolves and creates the application-owned directories.
// Windows: %APPDATA%\com.apirequest.app
// macOS: ~/Library/Application Support/com.apirequest.app
func ResolvePaths() (Paths, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return Paths{}, fmt.Errorf("resolve user config directory: %w", err)
	}
	return EnsurePaths(filepath.Join(base, appDirName))
}

// EnsurePaths creates all application-owned directories below dataDir. It is
// also the deterministic entry point used by tests and portable CLI setups.
func EnsurePaths(dataDir string) (Paths, error) {
	if dataDir == "" {
		return Paths{}, errors.New("application data directory is required")
	}
	dataDir = filepath.Clean(dataDir)
	paths := Paths{
		Data:   dataDir,
		Blobs:  filepath.Join(dataDir, "blobs"),
		Logs:   filepath.Join(dataDir, "logs"),
		Protos: filepath.Join(dataDir, "protos"),
	}
	for _, dir := range []string{paths.Data, paths.Blobs, paths.Logs, paths.Protos} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return Paths{}, fmt.Errorf("create application directory %q: %w", dir, err)
		}
	}
	return paths, nil
}

// DataDir returns the application data directory and preserves the original
// Phase 1 API for existing callers.
func DataDir() (string, error) {
	paths, err := ResolvePaths()
	if err != nil {
		return "", err
	}
	return paths.Data, nil
}
