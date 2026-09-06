// UpdateApi 更新检查域（ADR-018 验证链）：拉取渠道 manifest → ed25519 验签 →
// 版本/floor 比对。只做"检查与判定"，不下载安装包、不做静默替换（ADR-018：
// 替换与回滚属后续实施排期）。默认不启用——设置页配置更新源后才生效。
package binding

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"apirequest/backend/model"
	"apirequest/backend/platform"
	"apirequest/backend/storage"
	"apirequest/backend/updater"
	"apirequest/backend/version"
)

// pinnedUpdatePublicKey 内置发布公钥（hex）。发布侧用 cmd/updatetool keygen 生成
// 后替换此常量；测试可经 setting "update.publicKey" 覆盖。
const pinnedUpdatePublicKey = ""

const (
	settingUpdateManifestURL = "update.manifestUrl"
	settingUpdatePublicKey   = "update.publicKey"
)

// UpdateApi 更新检查
type UpdateApi struct {
	store *storage.Store
}

// NewUpdateApi 构造
func NewUpdateApi(store *storage.Store) *UpdateApi { return &UpdateApi{store: store} }

// ApplyVerifiedUpdate 编排 ADR-018 全链路：检查 → 下载 → 校验 → 换入/登记。
// unix：直接换入（调用方提示用户重启应用）；windows：写 pending-update 标记，
// 重启时由 startup 的 ApplyPendingUpdate 完成。需要先配置更新源。
func (a *UpdateApi) ApplyVerifiedUpdate() (*updater.CheckResult, error) {
	check, err := a.CheckForUpdates()
	if err != nil {
		return nil, err
	}
	if check.Status != updater.StatusAvailable || check.DownloadURL == "" {
		return check, nil
	}
	paths, pathErr := platform.ResolvePaths()
	if pathErr != nil {
		return nil, model.WrapError(model.KindStorage, pathErr)
	}
	updatesDir := filepath.Join(paths.Data, "updates")
	pkgName := fmt.Sprintf("apirequest-%s-%s%s", runtime.GOOS, runtime.GOARCH, installerExt())
	pkgPath, err := updater.Download(updater.DownloadConfig{
		URL:       check.DownloadURL,
		SHA256Hex: check.SHA256,
		DestDir:   updatesDir,
		Name:      pkgName,
	})
	if err != nil {
		return nil, err
	}
	exePath, exeErr := os.Executable()
	if exeErr != nil {
		return nil, model.WrapError(model.KindStorage, exeErr)
	}
	if runtime.GOOS == "windows" {
		err = updater.StagePendingUpdate(updater.PendingUpdate{
			PackagePath: pkgPath, TargetPath: exePath, SHA256Hex: check.SHA256,
		})
		check.Detail = "已就绪；重启应用后自动完成更新"
	} else {
		err = updater.ApplyNow(pkgPath, exePath, check.SHA256)
		check.Detail = "已换入新版本；重启应用即生效"
	}
	if err != nil {
		return check, err
	}
	return check, nil
}

// installerExt 平台安装包后缀（manifest 条目命名约定）
func installerExt() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// CheckForUpdates 执行一次签名验证的更新检查。
// 未配置更新源（update.manifestUrl setting）时返回 Validation 错误——
// 功能默认关闭，避免对任何未经确认的地址发起请求。
func (a *UpdateApi) CheckForUpdates() (*updater.CheckResult, error) {
	manifestURL, err := a.store.GetSetting(settingUpdateManifestURL)
	if err != nil {
		return nil, model.WrapError(model.KindStorage, err)
	}
	if manifestURL == "" {
		return nil, model.NewError(model.KindValidation,
			"update source not configured; set update.manifestUrl in settings first")
	}
	pub := ed25519.PublicKey{}
	if pinnedUpdatePublicKey != "" {
		raw, hexErr := hex.DecodeString(pinnedUpdatePublicKey)
		if hexErr != nil || len(raw) != ed25519.PublicKeySize {
			return nil, model.NewError(model.KindValidation, "pinned update public key is invalid")
		}
		pub = ed25519.PublicKey(raw)
	}
	if override, err := a.store.GetSetting(settingUpdatePublicKey); err == nil && override != "" {
		raw, hexErr := hex.DecodeString(override)
		if hexErr != nil || len(raw) != ed25519.PublicKeySize {
			return nil, model.NewError(model.KindValidation, "update.publicKey override is invalid")
		}
		pub = ed25519.PublicKey(raw)
	}
	return updater.Check(context.Background(), updater.Config{
		ManifestURL:    manifestURL,
		PublicKey:      pub,
		CurrentVersion: version.Version,
	})
}
