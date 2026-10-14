package binding

import (
	"apirequest/backend/model"
	"apirequest/backend/runner"
)

// ExportReportHTML 导出报告 HTML（自包含单文件，浏览器直接打开）。
// 渲染逻辑已下沉领域包：binding 只做缓存取数与转调。
func (a *RunnerApi) ExportReportHTML(workspaceId, runId string) (string, error) {
	report, err := a.reportForExport(workspaceId, runId)
	if err != nil {
		return "", err
	}
	out, err := runner.RenderReportHTML(report)
	if err != nil {
		return "", model.WrapError(model.KindValidation, err)
	}
	return out, nil
}
