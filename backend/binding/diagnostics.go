package binding

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"apirequest/backend/model"
	"apirequest/backend/secrets"
	"apirequest/backend/version"
)

// 诊断包导出（docs/ops.md）：用户报障时可一键生成环境快照。
// 只汇总环境/存储/网络的可观测状态，绝不包含请求内容与任何敏感值：
// 设置快照按白名单键导出（排除 proxy.password 这类 vault 引用），
// Vault/网络状态只带运行时摘要。
const diagnosticsSettingsWhitelist = "proxy.mode,proxy.url,proxy.username,retention.history,retention.runnerRuns,update.manifestUrl"

// DiagnosticsFilename 生成默认文件名（前端"另存为"对话框的初始名）
func DiagnosticsFilename(exportedAt time.Time) string {
	return "apirequest-diagnostics-" + exportedAt.UTC().Format("20060102-150405") + ".json"
}

// ExportDiagnostics 把诊断快照写入 path（JSON，UTF-8），返回实际路径。
func (a *SettingsApi) ExportDiagnostics(path string) (string, error) {
	if path == "" {
		return "", model.NewError(model.KindValidation, "diagnostics export path is required")
	}
	bundle, err := a.buildDiagnosticsBundle()
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(bundle, "", "  ")
	if err != nil {
		return "", model.WrapError(model.KindStorage, err)
	}
	// 0600：诊断包落在用户指定目录，仍收紧默认权限避免顺手带走敏感环境信息
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return "", model.WrapError(model.KindStorage, fmt.Errorf("write diagnostics: %w", err))
	}
	return path, nil
}

func (a *SettingsApi) buildDiagnosticsBundle() (map[string]any, error) {
	stats, err := a.store.StorageStats()
	if err != nil {
		return nil, model.WrapError(model.KindStorage, err)
	}
	// 白名单键逐一读取；不存在的键保留为空串以呈现"未配置"而非缺失
	keys := splitSettingKeys(diagnosticsSettingsWhitelist)
	values, err := a.store.GetSettings(keys)
	if err != nil {
		return nil, model.WrapError(model.KindStorage, err)
	}
	settings := map[string]string{}
	for _, key := range keys {
		v := values[key]
		if v == "" {
			continue
		}
		settings[key] = v
	}
	// 白名单外的键不会进来；防御性兜底——万一未来白名单加了带 vault 引用的键
	if err := diagnosticsContainsVaultRef(settings); err != nil {
		return nil, model.WrapError(model.KindStorage, err)
	}

	exportedAt := time.Now()
	status := a.GetNetworkStatus()
	meta := map[string]any{
		"schema":     1,
		"version":    version.Version,
		"goos":       runtime.GOOS,
		"goarch":     runtime.GOARCH,
		"exportedAt": exportedAt.UTC().Format(time.RFC3339),
	}
	// 冷启动耗时（预算 <1.5s，见 ops.md §5）；未记录则不带该键
	if ms := a.StartupMs(); ms > 0 {
		meta["startupMs"] = ms
	}
	bundle := map[string]any{
		"meta":        meta,
		"storage":     stats,
		"settings":    settings,
		"vaultStatus": a.store.Vault().Status(),
		"network":     status,
		"dataDir":     filepath.Dir(a.store.DbPath()),
		"dbPath":      filepath.Base(a.store.DbPath()),
	}
	return bundle, nil
}

func splitSettingKeys(csv string) []string {
	out := []string{}
	current := ""
	for _, r := range csv {
		if r == ',' {
			if current != "" {
				out = append(out, current)
			}
			current = ""
			continue
		}
		current += string(r)
	}
	if current != "" {
		out = append(out, current)
	}
	return out
}

// 供测试断言错误分类；同时防止误用：白名单里出现 vault 引用时导出会拒载
func diagnosticsContainsVaultRef(values map[string]string) error {
	for _, v := range values {
		if secrets.IsRef(v) {
			return errors.New("diagnostics settings snapshot must exclude vault references")
		}
	}
	return nil
}
