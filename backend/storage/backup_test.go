package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 备份 = VACUUM INTO 一致性快照；滚动保留 keep 份，旧的清理
func TestBackupRolling(t *testing.T) {
	s := openStoreWithMemoryKeyring(t, t.TempDir(), &memoryKeyring{values: map[string]string{}})
	if _, err := s.EnsureDefaultWorkspace(); err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(t.TempDir(), "backups")
	for i := 0; i < 3; i++ {
		path, err := s.Backup(backupDir, 2)
		if err != nil {
			t.Fatalf("backup %d: %v", i, err)
		}
		if !strings.HasSuffix(path, ".db") {
			t.Fatalf("backup path = %s", path)
		}
		info, err := os.Stat(path)
		if err != nil || info.Size() == 0 {
			t.Fatalf("backup %d missing/empty: %v", i, err)
		}
		// 文件名时间粒度到秒：强制时间戳不同
		future := time.Now().Add(time.Duration(i+1) * time.Second)
		_ = os.Chtimes(path, future, future)
		time.Sleep(1100 * time.Millisecond)
	}
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("kept %d backups, want 2 (rolling)", len(entries))
	}
}

// 同一天已备份则跳过（AutoBackup 的频控语义）
func TestAutoBackupSkipsWhenFresh(t *testing.T) {
	s := openStoreWithMemoryKeyring(t, t.TempDir(), &memoryKeyring{values: map[string]string{}})
	if _, err := s.EnsureDefaultWorkspace(); err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(t.TempDir(), "backups")
	if ran, err := s.AutoBackup(backupDir, 3); err != nil || !ran {
		t.Fatalf("first backup ran=%v err=%v", ran, err)
	}
	ran, err := s.AutoBackup(backupDir, 3)
	if err != nil {
		t.Fatal(err)
	}
	if ran {
		t.Fatal("second backup within 24h must be skipped")
	}
}
