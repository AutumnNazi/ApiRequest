package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"apirequest/backend/model"
	"apirequest/backend/storage"
)

// TestCliExportHeadless 编译 CLI 并无头导出集合为 Postman v2.1
func TestCliExportHeadless(t *testing.T) {
	if testing.Short() {
		t.Skip("skip in -short")
	}
	// 准备临时库：一个集合 + 一个请求。
	// 夹具完全不含敏感值（无 auth、无 Authorization/cookie 类 header）：
	// CLI 测试环境无 keyring 且文件 Vault 未解锁时，敏感值写入会失败（ErrLocked），
	// 那会把失败埋进数据准备阶段。脱敏路径由 binding 层单测覆盖。
	dataDir := t.TempDir()
	store, err := storage.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := store.EnsureDefaultWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	col, err := store.UpsertNode(model.Node{WorkspaceId: ws.Id, Kind: "collection", Name: "exp"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertNode(model.Node{
		WorkspaceId: ws.Id, ParentId: col.Id, Kind: "request", Name: "hit",
		Request: &model.HttpRequest{
			Method: "GET", Url: "https://api.test/hit",
			Headers:  []model.KV{{Key: "X-Trace", Value: "t1", Enabled: true}},
			Settings: model.DefaultSettings(),
		},
	}); err != nil {
		t.Fatal(err)
	}
	store.Close()

	bin := filepath.Join(t.TempDir(), "apirequest-cli.exe")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build cli: %v\n%s", err, out)
	}

	// 导出 stdout：--format postman
	out, err := exec.Command(bin, "export", "--collection", "exp", "--db", dataDir, "--format", "postman").Output()
	if err != nil {
		t.Fatalf("export: %v\n%s", err, out)
	}
	var payload struct {
		Info struct {
			Name string `json:"name"`
		} `json:"info"`
		Item []struct {
			Name string `json:"name"`
		} `json:"item"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("parse export: %v\n%s", err, out)
	}
	if payload.Info.Name != "exp" || len(payload.Item) != 1 || payload.Item[0].Name != "hit" {
		t.Fatalf("export content wrong: %s", out)
	}

	// --out 文件写出
	reportPath := filepath.Join(t.TempDir(), "col.json")
	if out, err := exec.Command(bin, "export", "--collection", "exp", "--db", dataDir,
		"--format", "openapi", "--out", reportPath).CombinedOutput(); err != nil {
		t.Fatalf("export --out: %v\n%s", err, out)
	}
	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "openapi") {
		t.Fatalf("openapi export missing marker: %s", data[:200])
	}

	// 未知格式报 2
	if err := exec.Command(bin, "export", "--collection", "exp", "--db", dataDir, "--format", "nope").Run(); err == nil {
		t.Fatal("unknown format must fail")
	} else if ee := (*exec.ExitError)(nil); errors.As(err, &ee) && ee.ExitCode() != 2 {
		t.Fatalf("unknown format exit = %v, want 2", err)
	}
}
