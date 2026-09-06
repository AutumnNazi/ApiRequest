package binding

import (
	"html/template"
	"strings"

	"apirequest/backend/model"
)

// HTML 报告导出（docs/advanced.md §2.1"可导出 JSON/HTML"）：
// 自包含单文件（内联样式、零外部资源，可直接发给同事或挂静态托管）。
// html/template 按上下文转义——请求名/错误等不可信字段不会注入 HTML/JS。
var runnerReportHTMLTmpl = template.Must(template.New("runner-report").Parse(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>ApiRequest Runner 报告 · {{.RunId}}</title>
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
  <div class="meta">run {{.RunId}} · {{.DurationMs}} ms 总耗时</div>
  <div class="summary">
    <div class="stat"><span class="label">Total</span><strong>{{.Total}}</strong></div>
    <div class="stat pass"><span class="label">Passed</span><strong>{{.Passed}}</strong></div>
    <div class="stat fail"><span class="label">Failed</span><strong>{{.Failed}}</strong></div>
    <div class="stat skip"><span class="label">Skipped</span><strong>{{.Skipped}}</strong></div>
    {{if .Canceled}}<div class="stat fail"><span class="label">Canceled</span><strong>yes</strong></div>{{end}}
  </div>
</div>
<div class="card">
  <table>
    <tr><th>轮次</th><th>请求</th><th>状态</th><th>耗时</th><th>结果</th></tr>
    {{range .Results}}
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

// ExportReportHTML 导出报告 HTML（自包含单文件，浏览器直接打开）
func (a *RunnerApi) ExportReportHTML(runId string) (string, error) {
	a.mu.Lock()
	report, ok := a.reports[runId]
	a.mu.Unlock()
	if !ok {
		return "", model.NewError(model.KindValidation, "no report for run: "+runId)
	}
	var buf strings.Builder
	if err := runnerReportHTMLTmpl.Execute(&buf, report); err != nil {
		return "", model.WrapError(model.KindValidation, err)
	}
	return buf.String(), nil
}
