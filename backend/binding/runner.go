package binding

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"

	"apirequest/backend/model"
	"apirequest/backend/runner"
	"apirequest/backend/storage"
)

// RunnerApi Collection Runner 域（docs/api-contract.md §4）
type RunnerApi struct {
	ctx     context.Context
	request *RequestApi
	store   *storage.Store

	mu      sync.Mutex
	running map[string]context.CancelFunc // runId → cancel
	reports map[string]*runner.Report     // runId → 最新报告（内存）
}

// NewRunnerApi 构造
func NewRunnerApi(request *RequestApi, store *storage.Store) *RunnerApi {
	return &RunnerApi{
		request: request,
		store:   store,
		running: map[string]context.CancelFunc{},
		reports: map[string]*runner.Report{},
	}
}

// startupRunner 注入 Wails context（包级 Startup 统一调）
func (a *RunnerApi) startup(ctx context.Context) { a.ctx = ctx }

// runnerProgress runner:progress 事件负载（docs/api-contract.md §5）
type runnerProgress struct {
	RunId       string `json:"runId"`
	Iteration   int    `json:"iteration"`
	RequestName string `json:"requestName"`
	Status      string `json:"status"` // pass | fail | skip
	Done        int    `json:"done"`
	Total       int    `json:"total"`
}

// RunCollection 同步执行集合并返回报告（长任务；进度经事件推送，可 CancelRun 中止）。
// runId 由前端生成。
func (a *RunnerApi) RunCollection(runId, workspaceId, collectionId string, opts runner.Options) (*runner.Report, error) {
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.running[runId] = cancel
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.running, runId)
		a.mu.Unlock()
	}()

	nodes, err := a.store.ListNodes(workspaceId)
	if err != nil {
		return nil, model.WrapError(model.KindStorage, err)
	}
	requests := runner.FlattenOrdered(collectionId, nodes)
	if len(requests) == 0 {
		return nil, model.NewError(model.KindValidation, "collection has no requests")
	}

	// 迭代行：数据文件优先，否则按 iterations 空行
	var rows []map[string]string
	if opts.DataFile != "" {
		rows, err = runner.ParseDataFile(opts.DataFile, opts.DataFormat)
		if err != nil {
			return nil, err
		}
	}
	if len(rows) == 0 {
		n := opts.Iterations
		if n <= 0 {
			n = 1
		}
		rows = make([]map[string]string, n)
	}

	report := &runner.Report{RunId: runId, Results: []runner.RequestResult{}}
	total := len(rows) * len(requests)
	done := 0
	start := time.Now()

	emit := func(iter int, name, status string) {
		done++
		if a.ctx != nil {
			wailsrt.EventsEmit(a.ctx, "runner:progress", runnerProgress{
				RunId: runId, Iteration: iter, RequestName: name,
				Status: status, Done: done, Total: total,
			})
		}
	}

loop:
	for iter, row := range rows {
		for _, node := range requests {
			select {
			case <-ctx.Done():
				report.Canceled = true
				break loop
			default:
			}

			rr := runner.RequestResult{
				Iteration: iter + 1, RequestName: node.Name, NodeId: node.Id,
			}
			// 数据行注入为最高优先级变量覆盖（data 作用域）
			sendCtx := model.SendContext{
				WorkspaceId: workspaceId, RequestId: node.Id,
				VariableOverrides: row,
			}
			res, serr := a.request.SendRequest(runId+"-"+node.Id, *node.Request, sendCtx)
			if serr != nil {
				rr.Failed = true
				rr.Error = serr.Error()
			} else {
				rr.Status = res.Status
				rr.DurationMs = int64(res.Timing.TotalMs)
				rr.TestResults = res.TestResults
				for _, t := range res.TestResults {
					if !t.Pass {
						rr.Failed = true
						break
					}
				}
			}
			report.Results = append(report.Results, rr)
			report.Total++
			if rr.Failed {
				report.Failed++
				emit(rr.Iteration, node.Name, "fail")
				if opts.StopOnError {
					break loop
				}
			} else {
				report.Passed++
				emit(rr.Iteration, node.Name, "pass")
			}
		}
	}
	report.Skipped = total - report.Total
	report.DurationMs = time.Since(start).Milliseconds()

	a.mu.Lock()
	a.reports[runId] = report
	a.mu.Unlock()
	return report, nil
}

// CancelRun 取消进行中的运行；未知 runId 为 no-op
func (a *RunnerApi) CancelRun(runId string) error {
	a.mu.Lock()
	cancel, ok := a.running[runId]
	a.mu.Unlock()
	if ok {
		cancel()
	}
	return nil
}

// ExportReport 导出报告 JSON（供 CI/存档）
func (a *RunnerApi) ExportReport(runId string) (string, error) {
	a.mu.Lock()
	report, ok := a.reports[runId]
	a.mu.Unlock()
	if !ok {
		return "", model.NewError(model.KindValidation, "no report for run: "+runId)
	}
	b, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", model.WrapError(model.KindValidation, err)
	}
	return string(b), nil
}
