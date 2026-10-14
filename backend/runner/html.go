package runner

import (
	"html/template"
	"strings"
)

// HTML 报告导出（docs/advanced.md §2.1"可导出 JSON/HTML"）：
// 自包含单文件（内联样式、零外部资源，可直接发给同事或挂静态托管）。
// html/template 按上下文转义——请求名/错误等不可信字段不会注入 HTML/JS。
// 领域包纯函数：GUI（binding.RunnerApi）与 CLI（cmd/cli）共用同一渲染。
var reportHTMLTmpl = template.Must(template.New("runner-report").Parse(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>ApiRequest Runner 报告 · {{.Report.RunId}}</title>
<style>
  :root { color-scheme: light; }
  body { font-family: system-ui, sans-serif; margin: 0; padding: 32px; background: #f8fafc; color: #0f172a; }
  .card { background: #fff; border: 1px solid #e2e8f0; border-radius: 12px; padding: 20px 24px; max-width: 900px; margin: 0 auto 16px; }
  h1 { font-size: 18px; margin: 0 0 4px; }
  .meta { color: #64748b; font-size: 12px; margin-bottom: 12px; }
  .summary { display: flex; gap: 12px; flex-wrap: wrap; }
  .stat { flex: 1; min-width: 100px; border: 1px solid #e2e8f0; border-radius: 8px; padding: 10px 14px; }
  .stat .label { font-size: 12px; color: #64748b; }
  .stat strong { font-size: 22px; display: block; }
  .pass strong { color: #16a34a; } .fail strong { color: #dc2626; } .skip strong { color: #94a3b8; }
  table { width: 100%; border-collapse: collapse; font-size: 13px; }
  th, td { text-align: left; padding: 8px 10px; border-bottom: 1px solid #eef2f7; vertical-align: top; }
  th { color: #64748b; font-weight: 600; font-size: 12px; }
  .status-ok { color: #16a34a; font-weight: 600; } .status-fail { color: #dc2626; font-weight: 600; }
  .err { color: #dc2626; font-size: 12px; margin-top: 4px; white-space: pre-wrap; }
  .tests { color: #475569; font-size: 12px; margin-top: 4px; }
</style>
</head>
<body>
<div class="card">
  <h1>ApiRequest Runner 报告</h1>
  <div class="meta">run {{.Report.RunId}} · {{.Report.DurationMs}} ms 总耗时</div>
  <div class="summary">
    <div class="stat"><span class="label">Total</span><strong>{{.Report.Total}}</strong></div>
    <div class="stat pass"><span class="label">Passed</span><strong>{{.Report.Passed}}</strong></div>
    <div class="stat fail"><span class="label">Failed</span><strong>{{.Report.Failed}}</strong></div>
    <div class="stat skip"><span class="label">Skipped</span><strong>{{.Report.Skipped}}</strong></div>
    {{if .Report.Canceled}}<div class="stat fail"><span class="label">Canceled</span><strong>yes</strong></div>{{end}}
  </div>
</div>
{{if .Stats.Executed}}
<div class="card">
  <h1 style="font-size:15px">性能统计（{{.Stats.Executed}} 个已执行请求）</h1>
  <div class="summary">
    <div class="stat"><span class="label">RPS</span><strong>{{printf "%.1f" .Stats.Rps}}</strong></div>
    <div class="stat"><span class="label">错误率</span><strong>{{printf "%.1f" .Stats.ErrorRatePct}}%</strong></div>
    <div class="stat"><span class="label">最快</span><strong>{{.Stats.MinMs}} ms</strong></div>
    <div class="stat"><span class="label">最慢</span><strong>{{.Stats.MaxMs}} ms</strong></div>
  </div>
  <div class="summary" style="margin-top:12px">
    <div class="stat"><span class="label">P50</span><strong>{{.Stats.P50Ms}} ms</strong></div>
    <div class="stat"><span class="label">P90</span><strong>{{.Stats.P90Ms}} ms</strong></div>
    <div class="stat"><span class="label">P95</span><strong>{{.Stats.P95Ms}} ms</strong></div>
    <div class="stat"><span class="label">P99</span><strong>{{.Stats.P99Ms}} ms</strong></div>
  </div>
</div>
{{end}}
<div class="card">
  <table>
    <tr><th>轮次</th><th>请求</th><th>状态</th><th>耗时</th><th>结果</th></tr>
    {{range .Report.Results}}
    <tr>
      <td>{{.Iteration}}</td>
      <td>{{.RequestName}}</td>
      <td>{{if .Status}}{{.Status}}{{else}}-{{end}}</td>
      <td>{{if .Status}}{{.DurationMs}} ms{{else}}-{{end}}</td>
      <td>
        {{if .Failed}}<span class="status-fail">FAIL</span>{{else}}<span class="status-ok">PASS</span>{{end}}
        {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
        {{if .TestResults}}<div class="tests">{{range .TestResults}}[{{if .Pass}}✓{{else}}✗{{end}}] {{.Name}}{{if .Error}} — {{.Error}}{{end}}<br>{{end}}</div>{{end}}
      </td>
    </tr>
    {{end}}
  </table>
</div>
</body>
</html>
`))

// htmlView 模板根对象：报告 + 性能统计
type htmlView struct {
	Report *Report
	Stats  Stats
}

// RenderReportHTML 渲染报告为自包含 HTML（零依赖纯函数，CLI 与 GUI 共用）。
func RenderReportHTML(report *Report) (string, error) {
	var buf strings.Builder
	if err := reportHTMLTmpl.Execute(&buf, htmlView{Report: report, Stats: ComputeStats(report)}); err != nil {
		return "", err
	}
	return buf.String(), nil
}
