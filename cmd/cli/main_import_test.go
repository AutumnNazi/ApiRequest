package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"apirequest/backend/model"
	"apirequest/backend/storage"
)

// TestCliImportHeadless 编译 CLI 并无头导入 Postman 集合
func TestCliImportHeadless(t *testing.T) {
	if testing.Short() {
		t.Skip("skip in -short")
	}
	dataDir := t.TempDir()
	store, err := storage.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnsureDefaultWorkspace(); err != nil {
		t.Fatal(err)
	}
	store.Close()

	bin := filepath.Join(t.TempDir(), "apirequest-cli.exe")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build cli: %v\n%s", err, out)
	}

	// 无敏感值夹具：CI 无 keyring 时 secret 写入会失败
	payload := `{
		"info": {"name": "Imported", "schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"item": [{"name": "ping", "request": {"method": "GET", "url": {"raw": "https://api.test/ping"}}}]
	}`
	srcPath := filepath.Join(t.TempDir(), "col.json")
	if err := os.WriteFile(srcPath, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}

	// import + 立即验证：stdout 输出 JSON 摘要（collection id + 环境数）
	out, err := exec.Command(bin, "import", "--file", srcPath, "--db", dataDir, "--format", "postman").Output()
	if err != nil {
		t.Fatalf("import: %v\n%s", err, out)
	}
	var summary struct {
		CollectionId string `json:"collectionId"`
		Name         string `json:"name"`
		Requests     int    `json:"requests"`
		Environments []any  `json:"environments"`
	}
	if err := json.Unmarshal(out, &summary); err != nil {
		t.Fatalf("parse import summary: %v\n%s", err, out)
	}
	if summary.CollectionId == "" || summary.Name != "Imported" || summary.Requests != 1 {
		t.Fatalf("import summary wrong: %s", out)
	}

	// 落库验证：重开库能读到导入的集合与请求
	reopened, err := storage.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	wsList, err := reopened.ListWorkspaces()
	if err != nil || len(wsList) == 0 {
		t.Fatalf("list workspaces: %v", err)
	}
	nodes, err := reopened.ListNodes(wsList[0].Id)
	if err != nil {
		t.Fatal(err)
	}
	var col, req *model.Node
	for i := range nodes {
		switch nodes[i].Name {
		case "Imported":
			col = &nodes[i]
		case "ping":
			req = &nodes[i]
		}
	}
	if col == nil || col.Kind != "collection" {
		t.Fatalf("imported collection missing: %+v", nodes)
	}
	if req == nil || req.Kind != "request" || req.ParentId != col.Id {
		t.Fatalf("imported request missing or misparented: %+v", nodes)
	}

	// auto 识别：--format 缺省也能走通（重复导入另一份文件）
	payload2 := `{"info":{"name":"Second","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},"item":[]}`
	src2 := filepath.Join(t.TempDir(), "col2.json")
	if err := os.WriteFile(src2, []byte(payload2), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(bin, "import", "--file", src2, "--db", dataDir).Output(); err != nil {
		t.Fatalf("auto import: %v\n%s", err, out)
	}
}
