package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsurePathsCreatesApplicationRoots(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "nested", "app")
	paths, err := EnsurePaths(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	want := Paths{
		Data:   filepath.Clean(dataDir),
		Blobs:  filepath.Join(dataDir, "blobs"),
		Logs:   filepath.Join(dataDir, "logs"),
		Protos: filepath.Join(dataDir, "protos"),
	}
	if paths != want {
		t.Fatalf("paths = %+v, want %+v", paths, want)
	}
	for _, path := range []string{paths.Data, paths.Blobs, paths.Logs, paths.Protos} {
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			t.Fatalf("application root %q is not a directory: info=%v err=%v", path, info, err)
		}
	}
}

func TestEnsurePathsRejectsInvalidRoot(t *testing.T) {
	if _, err := EnsurePaths(""); err == nil {
		t.Fatal("empty application data directory was accepted")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsurePaths(file); err == nil {
		t.Fatal("regular file was accepted as the application data directory")
	}
}
