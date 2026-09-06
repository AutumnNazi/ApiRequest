package binding

import (
	"strings"
	"testing"

	"apirequest/backend/model"
	"apirequest/backend/runner"
)

// HTML 报告导出：自包含单文件（内联 CSS、无外部资源），
// 汇总块 + 每请求明细表；转义不可信字段（名称/错误）
func TestExportReportHTML(t *testing.T) {
	api, _, _ := newRunnerApiWithStore(t)
	rep := &runner.Report{
		RunId: "html-run", Total: 3, Passed: 2, Failed: 1, Skipped: 1, DurationMs: 1234,
		Results: []runner.RequestResult{
			{Iteration: 1, RequestName: "ok-request <ok>", NodeId: "n1", Status: 200, DurationMs: 5},
			{Iteration: 1, RequestName: "bad", NodeId: "n2", Status: 500, DurationMs: 7, Failed: true,
				Error: "boom <script>alert(1)</script>",
				TestResults: []model.TestResult{{Name: "status ok", Pass: false, Error: "500 != 2xx"}}},
			{Iteration: 2, RequestName: "ok-request <ok>", NodeId: "n1", Status: 200, DurationMs: 4},
		},
	}
	api.rememberReport(rep)

	html, err := api.ExportReportHTML("html-run")
	if err != nil {
		t.Fatalf("export html: %v", err)
	}

	// 结构：DOCTYPE + 汇总数字 + 明细行
	for _, want := range []string{
		"<!DOCTYPE html>",
		">Total</span><strong>3</strong>", ">Passed</span><strong>2</strong>",
		">Failed</span><strong>1</strong>", ">Skipped</span><strong>1</strong>",
		"bad", "ok-request", "boom", "status ok",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("html missing %q:\n%.400s", want, html)
		}
	}
	// XSS 防护：请求名与错误里的尖括号必须转义，不能出现可执行的 script 标签
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Fatal("html injection: raw <script> leaked")
	}
	if !strings.Contains(html, "&lt;ok&gt;") {
		t.Fatal("request name angle brackets must be escaped")
	}
	// 未知 run 报错
	if _, err := api.ExportReportHTML("missing"); err == nil {
		t.Fatal("unknown run must error")
	}
}
