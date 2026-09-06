// 替换与回滚（ADR-018）：下载（sha256 校验）→ 两阶段替换。
// Windows 上运行中的 exe 被锁定，替换采用 pending-update 标记 + 启动时应用；
// Unix 直接同卷 rename 原子换入，失败恢复备份。标记在本层处理：完成或回滚后清除。
package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"apirequest/backend/model"
)

const downloadLimit = 512 << 20 // 安装包上限 512 MiB

// DownloadConfig 下载参数
type DownloadConfig struct {
	URL        string
	SHA256Hex  string
	DestDir    string // updates 缓存目录
	Name       string
	HTTPClient *http.Client
}

// Download 下载安装包并校验 sha256；失败不留半成品。返回最终包路径。
func Download(cfg DownloadConfig) (string, error) {
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	resp, err := client.Get(cfg.URL)
	if err != nil {
		return "", model.WrapError(model.KindNetwork, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", model.NewError(model.KindNetwork,
			fmt.Sprintf("download: GET %s → %s", cfg.URL, resp.Status))
	}
	if err := os.MkdirAll(cfg.DestDir, 0o700); err != nil {
		return "", model.WrapError(model.KindStorage, err)
	}
	tmp, err := os.CreateTemp(cfg.DestDir, ".download-*.tmp")
	if err != nil {
		return "", model.WrapError(model.KindStorage, err)
	}
	tmpPath := tmp.Name()
	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, hasher), io.LimitReader(resp.Body, downloadLimit+1))
	closeErr := tmp.Close()
	if err != nil || closeErr != nil {
		_ = os.Remove(tmpPath)
		return "", model.WrapError(model.KindNetwork, fmt.Errorf("download write: %v", err))
	}
	if size > downloadLimit {
		_ = os.Remove(tmpPath)
		return "", model.NewError(model.KindImport, "installer exceeds 512 MiB limit")
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); !strings.EqualFold(got, cfg.SHA256Hex) {
		_ = os.Remove(tmpPath)
		return "", model.NewError(model.KindImport, "sha256 mismatch: got "+got)
	}
	final := filepath.Join(cfg.DestDir, cfg.Name)
	if err := os.Rename(tmpPath, final); err != nil {
		_ = os.Remove(tmpPath)
		return "", model.WrapError(model.KindStorage, err)
	}
	return final, nil
}

// PendingUpdate 待应用的替换登记（pending-update.json）
type PendingUpdate struct {
	PackagePath string `json:"packagePath"`
	TargetPath  string `json:"targetPath"`
	SHA256Hex   string `json:"sha256Hex"`
}

func pendingMarkerPath(targetDir string) string {
	return filepath.Join(targetDir, "pending-update.json")
}

// markerDir 从目标路径取目录
func markerDir(targetPath string) string { return filepath.Dir(targetPath) }

// StagePendingUpdate 登记待应用替换（Windows：当前 exe 被锁定，重启后由
// ApplyPendingUpdate 完成；Unix 由调用方选择直接换入或同样走登记路径）
func StagePendingUpdate(pending PendingUpdate) error {
	if pending.PackagePath == "" || pending.TargetPath == "" {
		return model.NewError(model.KindValidation, "pending update needs package and target paths")
	}
	data, err := json.MarshalIndent(pending, "", "  ")
	if err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	return os.WriteFile(pendingMarkerPath(markerDir(pending.TargetPath)), data, 0o600)
}

// readPending 读取并删除语义由 Apply 决定；这里只读
func readPending(targetPath string) (PendingUpdate, error) {
	var pending PendingUpdate
	raw, err := os.ReadFile(pendingMarkerPath(filepath.Dir(targetPath)))
	if err != nil {
		return pending, err
	}
	err = json.Unmarshal(raw, &pending)
	return pending, err
}

func clearPending(targetPath string) {
	_ = os.Remove(pendingMarkerPath(filepath.Dir(targetPath)))
}

// verifyFile 校验文件 sha256
func verifyFile(path, wantHex string) error {
	f, err := os.Open(path)
	if err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	defer f.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); !strings.EqualFold(got, wantHex) {
		return model.NewError(model.KindImport, "sha256 mismatch: got "+got)
	}
	return nil
}

// StageDirectSwap unix 快速路径：备份旧二进制 → 原子换入 → 失败恢复备份。
// targetPath 当前内容备份为 <target>.bak。
func StageDirectSwap(pkgPath, targetPath string) error {
	backup := targetPath + ".bak"
	if err := os.Rename(targetPath, backup); err != nil {
		return model.WrapError(model.KindStorage, fmt.Errorf("backup current binary: %w", err))
	}
	if err := os.Rename(pkgPath, targetPath); err != nil {
		// 恢复备份
		_ = os.Rename(backup, targetPath)
		return model.WrapError(model.KindStorage, fmt.Errorf("swap in new binary: %w", err))
	}
	return nil
}

// ApplyPendingUpdate 启动时应用登记的替换：校验包 → 备份当前 → 换入 → 清标记。
// 包损坏等失败时回滚（保持当前二进制）并清除标记（错误返回给调用方记录）。
// 无 pending 标记时返回 nil（no-op）。exePath 由调用方传入（os.Executable()），
// 显式传参以便测试注入临时目录。
func ApplyPendingUpdate(exePath string) error {
	var err error
	exePath, err = filepath.EvalSymlinks(exePath)
	if err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	marker := pendingMarkerPath(filepath.Dir(exePath))
	if _, err := os.Stat(marker); os.IsNotExist(err) {
		return nil
	}
	pending, err := readPending(exePath)
	if err != nil {
		// 标记损坏：清除，避免每次启动报错
		clearPending(exePath)
		return model.WrapError(model.KindStorage, fmt.Errorf("pending-update marker corrupt: %w", err))
	}
	if err := verifyFile(pending.PackagePath, pending.SHA256Hex); err != nil {
		clearPending(exePath)
		return err
	}
	// 二次防御：目标路径必须与登记时一致（防标记被复制到别的安装）
	if pending.TargetPath != exePath {
		clearPending(exePath)
		return model.NewError(model.KindValidation, "pending update target mismatch")
	}
	if err := StageDirectSwap(pending.PackagePath, exePath); err != nil {
		clearPending(exePath)
		return err
	}
	clearPending(exePath)
	return nil
}

// ApplyNow unix 立即路径：校验 → 直接换入（调用方随后自行重启）。
// Windows 返回明确错误（运行中 exe 锁定，须走 pending 标记重启路径）。
func ApplyNow(pkgPath, targetPath, sha256Hex string) error {
	if runtime.GOOS == "windows" {
		return model.NewError(model.KindValidation,
			"windows requires the pending-update restart flow; call StagePendingUpdate instead")
	}
	if err := verifyFile(pkgPath, sha256Hex); err != nil {
		return err
	}
	return StageDirectSwap(pkgPath, targetPath)
}
