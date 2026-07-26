package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"apirequest/backend/model"
	"apirequest/backend/storage"
)

// TestCliRunEndToEnd 编译 CLI 并对临时库中的集合做真实运行
func TestCliRunEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skip in -short")
	}
	// mock 目标服务：/ok 返回 200，/fail 返回 500
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fail" {
			w.WriteHeader(500)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// 准备临时库
	dataDir := t.TempDir()
	store, err := storage.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := store.EnsureDefaultWorkspace()
	col, _ := store.UpsertNode(model.Node{WorkspaceId: ws.Id, Kind: "collection", Name: "smoke"})
	mkReq := func(name, path, testScript string, order float64) {
		store.UpsertNode(model.Node{
			WorkspaceId: ws.Id, ParentId: col.Id, Kind: "request", Name: name, SortOrder: order,
			Request: &model.HttpRequest{
				Method: "GET", Url: srv.URL + path,
				Settings: model.DefaultSettings(), TestScript: testScript,
			},
		})
	}
	mkReq("ok", "/ok", `pm.test('200', function(){ pm.expect(pm.response.code).to.equal(200); });`, 10)
	mkReq("fails", "/fail", `pm.test('should be 200', function(){ pm.expect(pm.response.code).to.equal(200); });`, 20)
	store.Close()

	// 编译 CLI
	bin := filepath.Join(t.TempDir(), "apirequest-cli.exe")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build cli: %v\n%s", err, out)
	}

	// list 子命令
	out, err := exec.Command(bin, "list", "--db", dataDir).CombinedOutput()
	if err != nil {
		t.Fatalf("list: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "smoke") {
		t.Errorf("list output missing collection: %s", out)
	}

	// run 子命令：1 个断言失败 → 退出码 1
	reportPath := filepath.Join(t.TempDir(), "report.json")
	runCmd := exec.Command(bin, "run", "--collection", "smoke", "--db", dataDir, "--report", reportPath)
	runOut, runErr := runCmd.Output()
	exitCode := 0
	if ee, ok := runErr.(*exec.ExitError); ok {
		exitCode = ee.ExitCode()
	} else if runErr != nil {
		t.Fatalf("run: %v", runErr)
	}
	if exitCode != 1 {
		t.Errorf("exit code = %d, want 1 (one failed request)", exitCode)
	}

	// stdout 报告可解析且计数正确
	var report struct {
		Total, Passed, Failed int
		Results               []struct {
			RequestName string
			Failed      bool
		}
	}
	if err := json.Unmarshal(runOut, &report); err != nil {
		t.Fatalf("parse report: %v\n%s", err, runOut)
	}
	if report.Total != 2 || report.Passed != 1 || report.Failed != 1 {
		t.Errorf("report = %+v", report)
	}
	// --report 文件也应写出
	if _, err := os.Stat(reportPath); err != nil {
		t.Errorf("report file: %v", err)
	}
}
