package binding

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDialogReadTextFileBoundsAndEncoding(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	dialog := NewDialogApi()
	dialog.startup(context.Background())

	valid := filepath.Join(dir, "valid.json")
	if err := os.WriteFile(valid, []byte(`{"ok":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := dialog.ReadTextFile(valid)
	if err != nil || got != `{"ok":true}` {
		t.Fatalf("valid file: got %q, err %v", got, err)
	}

	invalid := filepath.Join(dir, "invalid.txt")
	if err := os.WriteFile(invalid, []byte{0xff, 0xfe}, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := dialog.ReadTextFile(invalid); err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("invalid UTF-8 should be rejected, got %v", err)
	}

	tooLarge := filepath.Join(dir, "large.txt")
	file, err := os.Create(tooLarge)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxTextFileSize + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := dialog.ReadTextFile(tooLarge); err == nil || !strings.Contains(err.Error(), "32 MiB") {
		t.Fatalf("oversized file should be rejected, got %v", err)
	}

	if _, err := dialog.ReadTextFile(dir); err == nil {
		t.Fatal("directory should be rejected")
	}
}
