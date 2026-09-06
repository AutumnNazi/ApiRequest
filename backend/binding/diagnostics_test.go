package binding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"apirequest/backend/httpengine"
	"apirequest/backend/model"
)

// 诊断包导出：一个 JSON 文件汇总环境/存储/网络状态，且绝不包含敏感值
func TestExportDiagnostics(t *testing.T) {
	store, _ := openSettingsTestStore(t)
	if _, err := store.EnsureDefaultWorkspace(); err != nil {
		t.Fatal(err)
	}
	api := NewSettingsApi(store, httpengine.New())
	// 留一个敏感设置的痕迹：代理密码进 Vault，诊断包只能有引用不见明文
	if err := api.SetProxySettings(ProxySettings{
		Mode: "manual", Url: "http://user:secret-pw@proxy.example:8080",
	}); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(t.TempDir(), "diagnostics.json")
	if _, err := api.ExportDiagnostics(target); err != nil {
		t.Fatalf("export: %v", err)
	}
	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret-pw") {
		t.Fatal("diagnostics leaked proxy password")
	}
	var bundle map[string]any
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatalf("parse diagnostics: %v", err)
	}

	// 版本与平台快照
	meta, _ := bundle["meta"].(map[string]any)
	if meta["goos"] == "" || meta["version"] == "" || meta["exportedAt"] == nil {
		t.Fatalf("meta missing fields: %+v", meta)
	}

	// 存储统计
	storageSection, _ := bundle["storage"].(map[string]any)
	if storageSection["dbBytes"] == nil || storageSection["history"] == nil {
		t.Fatalf("storage section incomplete: %+v", storageSection)
	}

	// 设置快照：包含非敏感键、绝不含代理密码引用
	settingsSection, _ := bundle["settings"].(map[string]any)
	if settingsSection["proxy.mode"] != "manual" {
		t.Fatalf("settings snapshot missing proxy.mode: %+v", settingsSection)
	}
	if strings.Contains(string(raw), "vault://") {
		t.Fatal("diagnostics must not expose vault references")
	}

	// vault / 网络状态
	if bundle["vaultStatus"] == nil {
		t.Fatal("diagnostics missing vaultStatus")
	}
}

// 路径不可写时报结构化错误
func TestExportDiagnosticsRejectsBadPath(t *testing.T) {
	store, _ := openSettingsTestStore(t)
	api := NewSettingsApi(store, httpengine.New())
	bad := filepath.Join(t.TempDir(), "missing-dir", "diag.json")
	_, err := api.ExportDiagnostics(bad)
	if err == nil {
		t.Fatal("expected error for unwritable path")
	}
	var ae *model.AppError
	if !asAppErr(err, &ae) || ae.Kind != model.KindStorage {
		t.Fatalf("err = %v, want KindStorage AppError", err)
	}
}

func asAppErr(err error, target **model.AppError) bool {
	ae, ok := err.(*model.AppError)
	if ok {
		*target = ae
	}
	return ok
}

// 生成时间戳文件名助手（前端默认文件名）
func TestDiagnosticsDefaultFilename(t *testing.T) {
	name := DiagnosticsFilename(time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC))
	if name != "apirequest-diagnostics-20260906-120000.json" {
		t.Fatalf("filename = %q", name)
	}
}

// model 导入保留给未来扩展
var _ = model.KindStorage
