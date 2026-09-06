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
	// 两个环境：dev 激活，staging 未激活 —— list 应显示两者并标出激活态
	envDev, err := store.UpsertEnvironment(model.Environment{WorkspaceId: ws.Id, Name: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertEnvironment(model.Environment{WorkspaceId: ws.Id, Name: "staging"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetActiveEnvironment(ws.Id, envDev.Id); err != nil {
		t.Fatal(err)
	}
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
	if !strings.Contains(string(out), "env: dev") || !strings.Contains(string(out), "env: staging") {
		t.Errorf("list output missing environments: %s", out)
	}
	if !strings.Contains(string(out), "active") {
		t.Errorf("list output missing active marker: %s", out)
	}

	// run 子命令：1 个断言失败 → 退出码 1；--html 同步产出 HTML 报告
	reportPath := filepath.Join(t.TempDir(), "report.json")
	htmlPath := filepath.Join(t.TempDir(), "report.html")
	runCmd := exec.Command(bin, "run", "--collection", "smoke", "--db", dataDir, "--report", reportPath, "--html", htmlPath)
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
	// --html：自包含 HTML 落盘且含断言名
	htmlData, err := os.ReadFile(htmlPath)
	if err != nil {
		t.Fatalf("html report: %v", err)
	}
	if !strings.Contains(string(htmlData), "<!DOCTYPE html>") ||
		!strings.Contains(string(htmlData), "should be 200") {
		t.Errorf("html report content wrong: %.200s", htmlData)
	}
}

func TestParseEnvFile(t *testing.T) {
	m, err := parseEnvFile(`{"BASE_URL":"https://x.test","TOKEN":"abc"}`)
	if err != nil || m["BASE_URL"] != "https://x.test" || m["TOKEN"] != "abc" {
		t.Fatalf("parse = %v, %v", m, err)
	}
	if m, err := parseEnvFile(`{}`); err != nil || len(m) != 0 {
		t.Fatalf("empty object = %v, %v", m, err)
	}
	if _, err := parseEnvFile(`{"N":1}`); err == nil || !strings.Contains(err.Error(), "N") {
		t.Fatalf("number value err = %v", err)
	}
	if _, err := parseEnvFile(`[1,2]`); err == nil {
		t.Fatal("array root must be rejected")
	}
	if _, err := parseEnvFile(`not json`); err == nil {
		t.Fatal("malformed json must be rejected")
	}
}

func TestResolveEnvironment(t *testing.T) {
	store, err := storage.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ws, _ := store.EnsureDefaultWorkspace()
	dev, _ := store.UpsertEnvironment(model.Environment{
		WorkspaceId: ws.Id, Name: "dev",
		Variables: []model.Variable{{Key: "K", Value: "v", Enabled: true}},
	})
	if _, err := store.UpsertEnvironment(model.Environment{WorkspaceId: ws.Id, Name: "staging"}); err != nil {
		t.Fatal(err)
	}

	if id, err := resolveEnv(store, ws.Id, "dev"); err != nil || id != dev.Id {
		t.Fatalf("by name = %s, %v", id, err)
	}
	if id, err := resolveEnv(store, ws.Id, dev.Id); err != nil || id != dev.Id {
		t.Fatalf("by id = %s, %v", id, err)
	}
	if _, err := resolveEnv(store, ws.Id, "missing"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing err = %v", err)
	}
	dupA, _ := store.UpsertEnvironment(model.Environment{WorkspaceId: ws.Id, Name: "dup"})
	dupB, _ := store.UpsertEnvironment(model.Environment{WorkspaceId: ws.Id, Name: "dup"})
	if dupA.Id == dupB.Id {
		t.Fatal("setup: duplicates must have distinct ids")
	}
	if _, err := resolveEnv(store, ws.Id, "dup"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous err = %v", err)
	}
}

// TestCliRunEnvAndJunit 验证 --env-file/--env 变量注入与 --junit 报告落盘
func TestCliRunEnvAndJunit(t *testing.T) {
	if testing.Short() {
		t.Skip("skip in -short")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fail" {
			w.WriteHeader(500)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	dataDir := t.TempDir()
	store, err := storage.Open(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	ws, _ := store.EnsureDefaultWorkspace()
	col, _ := store.UpsertNode(model.Node{WorkspaceId: ws.Id, Kind: "collection", Name: "envsmoke"})
	// env-ok 只有在 {{BASE_URL}} 被注入后才可达，否则请求必然失败
	store.UpsertNode(model.Node{
		WorkspaceId: ws.Id, ParentId: col.Id, Kind: "request", Name: "env-ok", SortOrder: 10,
		Request: &model.HttpRequest{
			Method: "GET", Url: "{{BASE_URL}}/ok", Settings: model.DefaultSettings(),
			TestScript: `pm.test('200', function(){ pm.expect(pm.response.code).to.equal(200); });`,
		},
	})
	store.UpsertNode(model.Node{
		WorkspaceId: ws.Id, ParentId: col.Id, Kind: "request", Name: "always-fails", SortOrder: 20,
		Request: &model.HttpRequest{
			Method: "GET", Url: srv.URL + "/fail", Settings: model.DefaultSettings(),
			TestScript: `pm.test('should be 200', function(){ pm.expect(pm.response.code).to.equal(200); });`,
		},
	})
	// 环境不激活：证明 --env 显式选择（而非激活环境兜底）真正生效
	if _, err := store.UpsertEnvironment(model.Environment{
		WorkspaceId: ws.Id, Name: "dev",
		Variables: []model.Variable{{Key: "BASE_URL", Value: srv.URL, Enabled: true}},
	}); err != nil {
		t.Fatal(err)
	}
	store.Close()

	bin := filepath.Join(t.TempDir(), "apirequest-cli.exe")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = "."
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build cli: %v\n%s", err, out)
	}

	run := func(args ...string) (string, int) {
		cmd := exec.Command(bin, args...)
		out, err := cmd.Output()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		} else if err != nil {
			t.Fatalf("run %v: %v", args, err)
		}
		return string(out), code
	}
	var passedOf = func(out string) int {
		var report struct {
			Passed int
		}
		if err := json.Unmarshal([]byte(out), &report); err != nil {
			t.Fatalf("parse report: %v\n%s", err, out)
		}
		return report.Passed
	}

	// --env-file：BASE_URL 注入后 env-ok 可达（passed=1），always-fails 失败（退出码 1）
	envFile := filepath.Join(t.TempDir(), "env.json")
	if err := os.WriteFile(envFile, []byte(`{"BASE_URL":"`+srv.URL+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	junitPath := filepath.Join(t.TempDir(), "junit.xml")
	out, code := run("run", "--collection", "envsmoke", "--db", dataDir, "--env-file", envFile, "--junit", junitPath)
	if code != 1 || passedOf(out) != 1 {
		t.Errorf("--env-file run: exit=%d passed=%d out=%s", code, passedOf(out), out)
	}
	jdata, err := os.ReadFile(junitPath)
	if err != nil {
		t.Fatalf("junit file: %v", err)
	}
	if !strings.HasPrefix(string(jdata), "<?xml") || !strings.Contains(string(jdata), `failures="1"`) {
		t.Errorf("junit content = %s", jdata)
	}

	// --env <name>：同样注入 BASE_URL
	out, code = run("run", "--collection", "envsmoke", "--db", dataDir, "--env", "dev")
	if code != 1 || passedOf(out) != 1 {
		t.Errorf("--env run: exit=%d passed=%d out=%s", code, passedOf(out), out)
	}

	// 未注入时 env-ok 必然失败（证明上面的 passed=1 确实来自变量注入）
	out, code = run("run", "--collection", "envsmoke", "--db", dataDir)
	if code != 2 || passedOf(out) != 0 {
		t.Errorf("no-env run: exit=%d passed=%d", code, passedOf(out))
	}

	// --env 不存在 → 用法错误退出码 2
	if _, code = run("run", "--collection", "envsmoke", "--db", dataDir, "--env", "missing"); code != 2 {
		t.Errorf("--env missing: exit=%d, want 2", code)
	}
}
