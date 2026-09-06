package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// TestDownloadVerifiesSha256：下载 → sha256 校验 → 就位 updates/ 目录
func TestDownloadVerifiesSha256(t *testing.T) {
	pkg := []byte("installer-bytes-v2")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(pkg)
	}))
	defer srv.Close()

	dir := t.TempDir()
	got, err := Download(DownloadConfig{
		URL:       srv.URL + "/setup.exe",
		SHA256Hex: sha256Hex(pkg),
		DestDir:   dir,
		Name:      "setup.exe",
	})
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	data, err := os.ReadFile(got)
	if err != nil || string(data) != string(pkg) {
		t.Fatalf("downloaded = %v, %v", data, err)
	}
}

// sha256 不匹配：拒绝且不留半成品
func TestDownloadRejectsBadChecksum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("tampered"))
	}))
	defer srv.Close()

	dir := t.TempDir()
	_, err := Download(DownloadConfig{
		URL: srv.URL + "/setup.exe", SHA256Hex: sha256Hex([]byte("expected")),
		DestDir: dir, Name: "setup.exe",
	})
	if err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("err = %v, want checksum mismatch", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("failed download must leave nothing: %d entries", len(entries))
	}
}

// 两阶段替换 + 启动回滚闭环（对临时文件操作，不触碰真实运行中的二进制）
func TestStageAndApplyPendingUpdate(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "app.bin")
	current := []byte("current-binary-v1")
	next := []byte("next-binary-v2")
	if err := os.WriteFile(exePath, current, 0o755); err != nil {
		t.Fatal(err)
	}
	pkgPath := filepath.Join(dir, "pkg")
	if err := os.WriteFile(pkgPath, next, 0o755); err != nil {
		t.Fatal(err)
	}
	pkgSha := sha256Hex(next)

	// 阶段一：登记 pending（Windows 锁定场景的跨重启路径）
	if err := StagePendingUpdate(PendingUpdate{PackagePath: pkgPath, TargetPath: exePath, SHA256Hex: pkgSha}); err != nil {
		t.Fatalf("stage: %v", err)
	}

	// 启动时应用：校验 → 备份旧二进制 → 替换 → 清标记
	if err := ApplyPendingUpdate(exePath); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, _ := os.ReadFile(exePath)
	if string(got) != string(next) {
		t.Fatalf("exe not replaced")
	}
	backupPath := exePath + ".bak"
	backupData, err := os.ReadFile(backupPath)
	if err != nil || string(backupData) != string(current) {
		t.Fatalf("backup = %v, %v", backupData, err)
	}
	if _, err := os.Stat(pendingMarkerPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("marker must be cleared after apply: %v", err)
	}
}

// 损坏的包：启动应用 pending 时校验失败 → 回滚（保持当前二进制）且标记保留待排查？
// ADR-018：完成或回滚后清除标记。回滚 = 保持当前二进制 + 清标记 + 返回错误事实。
func TestApplyPendingUpdateRollsBackOnCorruptPackage(t *testing.T) {
	dir := t.TempDir()
	exePath := filepath.Join(dir, "app.bin")
	current := []byte("current-binary-v1")
	if err := os.WriteFile(exePath, current, 0o755); err != nil {
		t.Fatal(err)
	}
	pkgPath := filepath.Join(dir, "pkg-corrupt")
	if err := os.WriteFile(pkgPath, []byte("corrupted-download"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := StagePendingUpdate(PendingUpdate{
		PackagePath: pkgPath, TargetPath: exePath,
		SHA256Hex: sha256Hex([]byte("expected-but-different")),
	}); err != nil {
		t.Fatal(err)
	}

	err := ApplyPendingUpdate(exePath)
	if err == nil {
		t.Fatal("corrupt package must error")
	}
	got, _ := os.ReadFile(exePath)
	if string(got) != string(current) {
		t.Fatal("current binary must survive a corrupt package")
	}
	// 回滚后标记清除，不会每次启动重复报错
	if _, statErr := os.Stat(pendingMarkerPath(dir)); !os.IsNotExist(statErr) {
		t.Fatal("marker must be cleared after rollback")
	}
}

func TestPendingMarkerRoundTrip(t *testing.T) {
	dir := t.TempDir()
	pending := PendingUpdate{
		PackagePath: filepath.Join(dir, "pkg"),
		TargetPath:  filepath.Join(dir, "app"),
		SHA256Hex:   "abc",
	}
	if err := StagePendingUpdate(pending); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(pendingMarkerPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	var parsed PendingUpdate
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed != pending {
		t.Fatalf("parsed = %+v", parsed)
	}
}
