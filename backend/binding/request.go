// Package binding 是 Wails API 边界：把 core 能力以导出方法暴露给前端，
// 统一 AppError 序列化与事件推送（docs/api-contract.md）。
package binding

import (
	"context"
	"sync"
	"time"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"

	"apirequest/backend/httpengine"
	"apirequest/backend/model"
	"apirequest/backend/script"
	"apirequest/backend/storage"
	"apirequest/backend/template"
)

// RequestApi 请求执行域
type RequestApi struct {
	ctx    context.Context
	engine *httpengine.Engine
	store  *storage.Store

	mu      sync.Mutex
	inFlight map[string]context.CancelFunc // sendId → cancel
}

// NewRequestApi 构造
func NewRequestApi(engine *httpengine.Engine, store *storage.Store) *RequestApi {
	return &RequestApi{engine: engine, store: store, inFlight: map[string]context.CancelFunc{}}
}

// Startup 由 Wails OnStartup 注入运行时 context（事件推送用）。
// 用包级函数而非导出方法：绑定 struct 的导出方法会被 Wails 生成为前端绑定，
// context.Context 参数会产出非法 TS import。
func Startup(ctx context.Context, apis ...any) {
	for _, api := range apis {
		switch a := api.(type) {
		case *RequestApi:
			a.ctx = ctx
		case *RunnerApi:
			a.startup(ctx)
		case *MockApi:
			a.startup(ctx)
		case *ProtocolApi:
			a.startup(ctx)
		case *OAuth2Api:
			a.startup(ctx)
		}
	}
}

// nowUnixMs 事件时间戳
func nowUnixMs() int64 { return time.Now().UnixMilli() }

// progressPayload request:progress 事件负载
type progressPayload struct {
	SendId string `json:"sendId"`
	Phase  string `json:"phase"` // sending | done
}

func (a *RequestApi) emitProgress(sendId, phase string) {
	if a.ctx != nil {
		wailsrt.EventsEmit(a.ctx, "request:progress", progressPayload{SendId: sendId, Phase: phase})
	}
}

// SendRequest 执行完整请求生命周期（docs/request-lifecycle.md §1）：
// 收集上下文 → 前置脚本 → 变量解析 → 发送 → 测试脚本 → 变量持久化 → 落历史。
// sendId 由前端生成，用于关联进度事件与取消。
func (a *RequestApi) SendRequest(sendId string, req model.HttpRequest, sendCtx model.SendContext) (model.ResponseResult, error) {
	ctx, cancel := context.WithCancel(context.Background())
	a.mu.Lock()
	a.inFlight[sendId] = cancel
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.inFlight, sendId)
		a.mu.Unlock()
	}()

	var zero model.ResponseResult

	// 1. 收集上下文（变量作用域 + 脚本继承链）
	ec, err := collectContext(a.store, req, sendCtx)
	if err != nil {
		return zero, err
	}
	sandbox := script.NewSandbox(scriptTimeout, ec.scope.Snapshot(), ec.envVars, ec.colVars, ec.globalVars)

	// 2. 前置脚本（根→叶→请求级；可改请求与变量）
	sandbox.SetRequest(&req)
	for _, code := range ec.preScripts {
		if serr := sandbox.Run(code, "pre"); serr != nil {
			return zero, serr
		}
	}
	// 脚本 set 的变量并入作用域（最高优先级层之下已合并于 merged 视图）
	preResult := sandbox.Result()
	for _, c := range []*script.VarChanges{preResult.GlobalChanges, preResult.CollectionChanges, preResult.EnvChanges} {
		ec.scope.PushMap(c.Set)
		for k := range c.Unset {
			ec.scope.Unset(k)
		}
	}

	// 3. 变量解析（含集合级 auth 继承）
	resolveInheritedAuth(&req, ec.ancestors)
	resolved := template.ResolveRequest(req, ec.scope)

	// 3.5 Cookie Jar：为目标 host 注入存储的 cookie（用户已手写 Cookie 头则不覆盖）
	attachCookies(a.store, &resolved)

	// 4-5. 发送
	a.emitProgress(sendId, "sending")
	res, err := a.engine.Send(ctx, resolved)
	a.emitProgress(sendId, "done")
	if err != nil {
		return res, model.WrapError(model.KindNetwork, err)
	}

	// 5.5 响应 Set-Cookie 写回 Jar
	persistCookies(a.store, resolved.Url, res.Cookies)

	// 6. 测试脚本（继承链 + 请求级）
	sandbox.SetResponse(&res)
	var scriptErr error
	for _, code := range ec.testScripts {
		if serr := sandbox.Run(code, "test"); serr != nil {
			scriptErr = serr
			break // 脚本异常中止后续段；已收集的断言结果仍返回
		}
	}
	r := sandbox.Result()
	res.TestResults = r.TestResults
	res.ScriptLogs = r.Logs
	if scriptErr != nil {
		if ae, ok := scriptErr.(*model.AppError); ok {
			res.ScriptLogs = append(res.ScriptLogs, "[error] "+ae.Detail)
		}
	}

	// 7. 变量变更持久化（Go 统一提交）
	if perr := persistVariableChanges(a.store, ec, sendCtx.WorkspaceId, r); perr != nil {
		res.ScriptLogs = append(res.ScriptLogs, "[error] persist variables: "+perr.Error())
	}

	// 8. 落历史（存已解析请求快照；失败不阻断响应返回）
	histId, herr := a.store.InsertHistory(model.HistoryItem{
		WorkspaceId: sendCtx.WorkspaceId,
		RequestSnap: resolved,
		Status:      res.Status,
		DurationMs:  int64(res.Timing.TotalMs),
		SizeBytes:   res.SizeBytes,
		Timing:      res.Timing,
		RespHeaders: res.Headers,
		BodyInline:  res.Body.Text,
		TestResults: res.TestResults,
	})
	if herr == nil {
		res.HistoryId = histId
	}
	return res, nil
}

// CancelRequest 取消进行中的请求；未知/已完成的 sendId 为 no-op
func (a *RequestApi) CancelRequest(sendId string) error {
	a.mu.Lock()
	cancel, ok := a.inFlight[sendId]
	a.mu.Unlock()
	if ok {
		cancel()
	}
	return nil
}
