package binding

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"

	"apirequest/backend/model"
	"apirequest/backend/runner"
	"apirequest/backend/storage"
)

const maxCachedRunnerReports = 20

// runnerMaxNextRequestHops 单迭代行内 pm.setNextRequest 跳转的步数上限（防自环死循环）
const runnerMaxNextRequestHops = 200

// RunnerApi Collection Runner 域（docs/api-contract.md §4）
type RunnerApi struct {
	ctx     context.Context
	request *RequestApi
	store   *storage.Store

	operations  *operationRegistry
	mu          sync.Mutex
	reports     map[string]*runner.Report // runId → 最新报告（内存）
	reportOrder []string
	// lastNextRequest 最近一次 sendRequest 返回的 pm.setNextRequest 值
	//（execute 写、串行调度读；并发模式下多 worker 都会写，但流转控制只对串行有意义）
	lastNextRequest atomic.Value // string
}

// NewRunnerApi 构造
func NewRunnerApi(request *RequestApi, store *storage.Store) *RunnerApi {
	operations := newOperationRegistry()
	if request != nil && request.operations != nil {
		operations = request.operations
	}
	return &RunnerApi{
		request:    request,
		store:      store,
		operations: operations,
		reports:    map[string]*runner.Report{},
	}}

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
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, finish, err := a.operations.begin(parent, runId, workspaceId)
	if err != nil {
		// 关停（应用退出）是生命周期事件，报 Validation 会把它说成用户输入问题
		kind := model.KindValidation
		if IsRegistryClosing(err) {
			kind = model.KindNetwork
		}
		return nil, model.NewError(kind, err.Error())
	}
	defer finish()

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
	// 并发模式下 emit 会被多个 worker 同时调用：done 必须原子
	var done atomic.Int64
	start := time.Now()

	emit := func(iter int, name, status string) {
		current := done.Add(1)
		if a.ctx != nil {
			wailsrt.EventsEmit(a.ctx, "runner:progress", runnerProgress{
				RunId: runId, Iteration: iter, RequestName: name,
				Status: status, Done: int(current), Total: total,
			})
		}
	}

	// 任务列表：(迭代行, 请求) 对。串行按顺序执行；并发模式由工作协程池消费
	type task struct {
		iter int
		row  map[string]string
		node model.Node
	}
	tasks := make([]task, 0, total)
	for iter, row := range rows {
		for _, node := range requests {
			tasks = append(tasks, task{iter: iter, row: row, node: node})
		}
	}

	// nameIndex 按名称解析集合内请求（pm.setNextRequest 跳转用；重名取第一个）
	nameIndex := make(map[string]int, len(requests))
	for i, node := range requests {
		if _, exists := nameIndex[node.Name]; !exists {
			nameIndex[node.Name] = i
		}
	}

	var mu sync.Mutex
	canceled := false

	// execute 单个任务：发送、聚合结果（mu 保护）、StopOnError 触发取消
	execute := func(ctx context.Context, tk task) bool {
		select {
		case <-ctx.Done():
			return false
		default:
		}
		// think-time：任务开始前等待（串行=请求之间；并发=各 worker 的任务之间）。
		// 放 execute 而非调度方：两条路径共用，且 ctx 取消能立即打断等待
		if opts.DelayMs > 0 {
			select {
			case <-time.After(time.Duration(opts.DelayMs) * time.Millisecond):
			case <-ctx.Done():
				return false
			}
		}
		node := tk.node
		rr := runner.RequestResult{
			Iteration: tk.iter + 1, RequestName: node.Name, NodeId: node.Id,
		}
		// 数据行注入为最高优先级变量覆盖（data 作用域）；CLI --env-file 变量
		// 次之（同一 key 时数据行胜出），两者合并后整体高于环境变量
		sendCtx := model.SendContext{
			WorkspaceId: workspaceId, RequestId: node.Id,
			EnvironmentId:     opts.EnvId,
			VariableOverrides: runner.MergeOverrides(opts.EnvOverrides, tk.row),
		}
		requestSendId := fmt.Sprintf("%s-%d-%s", runId, tk.iter+1, node.Id)
		res, serr := a.request.sendRequest(ctx, requestSendId, *node.Request, sendCtx)
		if ctx.Err() != nil {
			// 取消时也须释放本轮已注册的响应 blob，否则泄漏到应用关闭
			if serr == nil {
				_ = a.request.releaseResponseBlob(res.Body.BlobRef)
			}
			mu.Lock()
			canceled = true
			mu.Unlock()
			return false
		}
		if serr != nil {
			rr.Failed = true
			rr.Error = serr.Error()
		} else {
			rr.Status = res.Status
			rr.DurationMs = int64(res.Timing.TotalMs)
			rr.TestResults = res.TestResults
			// 记录 pm.setNextRequest 的流转意图（串行调度读取；并发模式忽略）
			a.lastNextRequest.Store(res.NextRequest)
			for _, t := range res.TestResults {
				if !t.Pass {
					rr.Failed = true
					break
				}
			}
			_ = a.request.releaseResponseBlob(res.Body.BlobRef)
		}
		mu.Lock()
		report.Results = append(report.Results, rr)
		report.Total++
		status := "pass"
		if rr.Failed {
			report.Failed++
			status = "fail"
		} else {
			report.Passed++
		}
		mu.Unlock()
		emit(rr.Iteration, node.Name, status)
		if rr.Failed && opts.StopOnError {
			return false // 通知调度方停止派发后续任务
		}
		return true
	}

	concurrency := opts.Concurrency
	if concurrency > len(tasks) {
		concurrency = len(tasks)
	}
	if concurrency > 1 {
		// 并发压测模式：固定 worker 池；StopOnError 或取消时关闭任务通道
		taskCh := make(chan task)
		var wg sync.WaitGroup
		stopCh := make(chan struct{})
		for w := 0; w < concurrency; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for {
					select {
					case <-stopCh:
						return
					case tk, ok := <-taskCh:
						if !ok {
							return
						}
						if !execute(ctx, tk) {
							select {
							case <-stopCh:
							default:
								close(stopCh)
							}
							return
						}
					}
				}
			}()
		}
	dispatch:
		for _, tk := range tasks {
			select {
			case <-stopCh:
				break dispatch
			case <-ctx.Done():
				mu.Lock()
				canceled = true
				mu.Unlock()
				break dispatch
			case taskCh <- tk:
			}
		}
		close(taskCh)
		wg.Wait()
	} else {
		// 串行：按自然顺序走任务列表；pm.setNextRequest 命中名称时在当前迭代行内
		// 跳转（Postman 语义：流转不跨迭代行，可回跳形成循环）。
		// 只有偏离自然顺序的跳转才计入 hops 上限——自环（如 A→A）会在上限处终止，
		// 而超过上限数量的请求按自然顺序跑完不受影响。
		hops := 0
		for i := 0; i < len(tasks); {
			tk := tasks[i]
			if !execute(ctx, tk) {
				break
			}
			next, _ := a.lastNextRequest.Load().(string)
			i++ // 自然顺序默认前进一步
			if next == "" {
				continue
			}
			if idx, ok := nameIndex[next]; ok {
				// 任务列表按 (iter, 树序) 排列且每行结构相同：目标索引直接按行内偏移算
				if target := tk.iter*len(requests) + idx; target != i {
					i = target
					hops++
					if hops >= runnerMaxNextRequestHops {
						break
					}
				}
			}
			// 未命中名称：保持自然顺序
		}
	}
	if canceled {
		report.Canceled = true
	}
	// 结果按（迭代，树序）稳定排序：并发完成顺序不定，报告须可复现
	sort.SliceStable(report.Results, func(i, j int) bool {
		if report.Results[i].Iteration != report.Results[j].Iteration {
			return report.Results[i].Iteration < report.Results[j].Iteration
		}
		return report.Results[i].NodeId < report.Results[j].NodeId
	})
	report.Skipped = total - report.Total
	report.DurationMs = time.Since(start).Milliseconds()

	a.rememberReport(report)
	// 运行历史落库（docs/decisions.md ADR-015：翻转原"仅内存"决策；失败不阻断返回报告）
	if err := a.store.SaveRunnerRun(workspaceId, collectionId, report); err != nil {
		if a.ctx != nil {
			wailsrt.LogWarningf(a.ctx, "persist runner run %s: %v", runId, err)
		}
	}
	return report, nil
}

func (a *RunnerApi) rememberReport(report *runner.Report) {
	if report == nil || report.RunId == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.reports[report.RunId]; exists {
		for index, id := range a.reportOrder {
			if id == report.RunId {
				a.reportOrder = append(a.reportOrder[:index], a.reportOrder[index+1:]...)
				break
			}
		}
	}
	a.reports[report.RunId] = report
	a.reportOrder = append(a.reportOrder, report.RunId)
	for len(a.reportOrder) > maxCachedRunnerReports {
		oldest := a.reportOrder[0]
		a.reportOrder = a.reportOrder[1:]
		delete(a.reports, oldest)
	}
}

// CancelRun 取消进行中的运行；未知 runId 为 no-op
func (a *RunnerApi) CancelRun(runId string) error {
	a.operations.cancel(runId)
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

// ListRunnerRuns 查询持久化的运行历史摘要页（时间倒序）
func (a *RunnerApi) ListRunnerRuns(workspaceId string, q model.RunnerRunQuery) (model.RunnerRunPage, error) {
	return a.store.ListRunnerRuns(workspaceId, q)
}

// GetRunnerRun 取单次运行的完整报告（含明细），支持重启后查看
func (a *RunnerApi) GetRunnerRun(workspaceId, runId string) (*runner.Report, error) {
	return a.store.GetRunnerRun(workspaceId, runId)
}

// DeleteRunnerRun 删除单次运行历史
func (a *RunnerApi) DeleteRunnerRun(workspaceId, runId string) error {
	return a.store.DeleteRunnerRun(workspaceId, runId)
}

// ClearRunnerRuns 清空工作区全部运行历史
func (a *RunnerApi) ClearRunnerRuns(workspaceId string) error {
	return a.store.ClearRunnerRuns(workspaceId)
}
