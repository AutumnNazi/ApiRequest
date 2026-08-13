// Package binding 是 Wails API 边界：把 core 能力以导出方法暴露给前端，
// 统一 AppError 序列化与事件推送（docs/api-contract.md）。
package binding

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"
	"time"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"

	"apirequest/backend/httpengine"
	"apirequest/backend/model"
	"apirequest/backend/script"
	"apirequest/backend/secrets"
	"apirequest/backend/storage"
	"apirequest/backend/template"
)

// RequestApi 请求执行域
type RequestApi struct {
	ctx        context.Context
	engine     *httpengine.Engine
	store      *storage.Store
	operations *operationRegistry
	blobMu     sync.RWMutex
	liveBlobs  map[string]string // blob ref -> workspace id
}

// NewRequestApi 构造
func NewRequestApi(engine *httpengine.Engine, store *storage.Store) *RequestApi {
	return &RequestApi{
		engine: engine, store: store, operations: newOperationRegistry(), liveBlobs: map[string]string{},
	}
}

// Startup 由 Wails OnStartup 注入运行时 context（事件推送用）。
// 用包级函数而非导出方法：绑定 struct 的导出方法会被 Wails 生成为前端绑定，
// context.Context 参数会产出非法 TS import。
func Startup(ctx context.Context, apis ...any) {
	for _, api := range apis {
		switch a := api.(type) {
		case *RequestApi:
			a.ctx = ctx
		case *LifecycleApi:
			a.startup(ctx)
		case *RunnerApi:
			a.startup(ctx)
		case *MockApi:
			a.startup(ctx)
		case *ProtocolApi:
			a.startup(ctx)
		case *OAuth2Api:
			a.startup(ctx)
		case *GrpcApi:
			a.startup(ctx)
		case *GraphqlApi:
			a.startup(ctx)
		case *DialogApi:
			a.startup(ctx)
		}
	}
}

// nowUnixMs 事件时间戳
func nowUnixMs() int64 { return time.Now().UnixMilli() }

// progressPayload request:progress 事件负载
type progressPayload struct {
	SendId        string `json:"sendId"`
	Phase         string `json:"phase"` // sending | ttfb | downloading | done
	BytesReceived int64  `json:"bytesReceived"`
	TotalBytes    int64  `json:"totalBytes"`
}

func (a *RequestApi) emitProgress(sendId, phase string, bytesReceived, totalBytes int64) {
	if a.ctx != nil {
		wailsrt.EventsEmit(a.ctx, "request:progress", progressPayload{
			SendId: sendId, Phase: phase, BytesReceived: bytesReceived, TotalBytes: totalBytes,
		})
	}
}

// SendRequest 执行完整请求生命周期（docs/request-lifecycle.md §1）：
// 收集上下文 → 前置脚本 → 变量解析 → 发送 → 测试脚本 → 变量持久化 → 落历史。
// sendId 由前端生成，用于关联进度事件与取消。
func (a *RequestApi) SendRequest(sendId string, req model.HttpRequest, sendCtx model.SendContext) (model.ResponseResult, error) {
	parent := a.ctx
	if parent == nil {
		parent = context.Background()
	}
	return a.sendRequest(parent, sendId, req, sendCtx)
}

func (a *RequestApi) sendRequest(parent context.Context, sendId string, req model.HttpRequest, sendCtx model.SendContext) (model.ResponseResult, error) {
	if sendCtx.WorkspaceId == "" {
		return model.ResponseResult{}, model.NewError(model.KindValidation, "workspaceId is required")
	}
	ctx, finish, err := a.operations.begin(parent, sendId, sendCtx.WorkspaceId)
	if err != nil {
		return model.ResponseResult{}, model.NewError(model.KindValidation, err.Error())
	}
	defer finish()

	var zero model.ResponseResult

	// 1. 收集上下文（变量作用域 + 脚本继承链）
	ec, err := collectContext(a.store, req, sendCtx)
	if err != nil {
		return zero, err
	}
	sandbox := script.NewSandbox(scriptTimeout, ec.scope.Snapshot(), ec.envVars, ec.colVars, ec.globalVars)
	// pm.sendRequest 受控通道：直接走引擎（不递归整个生命周期，避免脚本套脚本）
	sandbox.SendFunc = func(sreq model.HttpRequest) (model.ResponseResult, error) {
		resolved := template.ResolveRequest(sreq, ec.scope)
		sctx, scancel := context.WithTimeout(ctx, 30*time.Second)
		defer scancel()
		return a.engine.SendTransient(sctx, resolved)
	}

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
	redactor := secrets.NewRedactor(a.store.Vault(), ec.secretValues...)
	for _, value := range secrets.RequestCredentialValues(resolved) {
		redactor.Add(value)
	}

	// 3.5 Cookie Jar：为目标 host 注入存储的 cookie（用户已手写 Cookie 头则不覆盖）
	if err := attachCookies(a.store, &resolved); err != nil {
		return zero, model.WrapError(model.KindStorage, fmt.Errorf("load cookies: %w", err))
	}

	// 4-5. 发送
	a.emitProgress(sendId, "sending", 0, 0)
	res, err := a.engine.SendWithProgress(ctx, resolved, func(progress httpengine.Progress) {
		a.emitProgress(sendId, progress.Phase, progress.BytesReceived, progress.TotalBytes)
	})
	a.emitProgress(sendId, "done", res.SizeBytes, res.SizeBytes)
	if err != nil {
		return res, model.WrapError(model.KindNetwork, err)
	}
	if res.Body.BlobRef != "" {
		a.blobMu.Lock()
		a.liveBlobs[res.Body.BlobRef] = sendCtx.WorkspaceId
		a.blobMu.Unlock()
	}
	for _, value := range secrets.HeaderValues(res.Headers) {
		redactor.Add(value)
	}

	// 5.5 响应 Set-Cookie 写回 Jar
	cookiePersistErr := persistCookies(a.store, resolved.Url, res.Cookies)

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
	if cookiePersistErr != nil {
		res.ScriptLogs = append(res.ScriptLogs, "[error] persist cookies: "+cookiePersistErr.Error())
	}
	if scriptErr != nil {
		if ae, ok := scriptErr.(*model.AppError); ok {
			res.ScriptLogs = append(res.ScriptLogs, "[error] "+ae.Detail)
		}
	}

	// 7. 变量变更持久化（Go 统一提交）
	if perr := persistVariableChanges(a.store, ec, sendCtx.WorkspaceId, r); perr != nil {
		res.ScriptLogs = append(res.ScriptLogs, "[error] persist variables: "+perr.Error())
	}
	res.ScriptLogs = redactor.Strings(res.ScriptLogs)
	res.TestResults = redactor.TestResults(res.TestResults)

	// 8. 落历史（存已解析请求快照；大响应仅作为 live Blob 返回，不进入审计历史）
	histItem := model.HistoryItem{
		WorkspaceId: sendCtx.WorkspaceId,
		RequestSnap: resolved,
		Status:      res.Status,
		DurationMs:  int64(res.Timing.TotalMs),
		SizeBytes:   res.SizeBytes,
		Timing:      res.Timing,
		RespHeaders: res.Headers,
		TestResults: res.TestResults,
	}
	if res.Body.Inline {
		histItem.BodyInline = res.Body.Text
	}
	histId, herr := a.store.InsertHistory(histItem)
	if herr == nil {
		res.HistoryId = histId
	} else {
		res.ScriptLogs = append(res.ScriptLogs, redactor.String("[warning] save history: "+herr.Error()))
	}
	return res, nil
}

// GetResponseBlobInfo reads metadata without loading the response body.
func (a *RequestApi) GetResponseBlobInfo(blobRef string) (model.ResponseBlobInfo, error) {
	a.blobMu.RLock()
	defer a.blobMu.RUnlock()
	if _, ok := a.liveBlobs[blobRef]; !ok {
		return model.ResponseBlobInfo{}, model.NewError(model.KindStorage, "response blob is not available")
	}
	size, err := a.store.BlobInfo(blobRef)
	if err != nil {
		return model.ResponseBlobInfo{}, model.WrapError(model.KindStorage, err)
	}
	return model.ResponseBlobInfo{Ref: blobRef, SizeBytes: size}, nil
}

// ReadResponseBlobRange returns a bounded binary-safe chunk.
func (a *RequestApi) ReadResponseBlobRange(blobRef string, offset, limit int64) (model.ResponseBlobChunk, error) {
	a.blobMu.RLock()
	defer a.blobMu.RUnlock()
	if _, ok := a.liveBlobs[blobRef]; !ok {
		return model.ResponseBlobChunk{}, model.NewError(model.KindStorage, "response blob is not available")
	}
	data, eof, err := a.store.ReadBlobRange(blobRef, offset, limit)
	if err != nil {
		return model.ResponseBlobChunk{}, model.WrapError(model.KindStorage, err)
	}
	return model.ResponseBlobChunk{
		Offset: offset, BytesRead: int64(len(data)), DataBase64: base64.StdEncoding.EncodeToString(data), Eof: eof,
	}, nil
}

// SaveResponseBlob streams a blob to a path selected through DialogApi.
func (a *RequestApi) SaveResponseBlob(blobRef, destination string) (int64, error) {
	a.blobMu.RLock()
	defer a.blobMu.RUnlock()
	if _, ok := a.liveBlobs[blobRef]; !ok {
		return 0, model.NewError(model.KindStorage, "response blob is not available")
	}
	written, err := a.store.CopyBlob(blobRef, destination)
	if err != nil {
		return 0, model.WrapError(model.KindStorage, err)
	}
	return written, nil
}

// ReleaseResponseBlob releases a live response body after its UI owner is gone.
func (a *RequestApi) ReleaseResponseBlob(blobRef string) error {
	if err := a.releaseResponseBlob(blobRef); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	return nil
}

func (a *RequestApi) releaseResponseBlob(blobRef string) error {
	a.blobMu.Lock()
	defer a.blobMu.Unlock()
	if _, ok := a.liveBlobs[blobRef]; !ok {
		return nil
	}
	if err := a.store.RemoveBlob(blobRef); err != nil {
		return err
	}
	delete(a.liveBlobs, blobRef)
	return nil
}

func (a *RequestApi) releaseWorkspaceBlobs(workspaceId string) error {
	a.blobMu.Lock()
	defer a.blobMu.Unlock()
	var firstErr error
	for ref, owner := range a.liveBlobs {
		if workspaceId != "" && owner != workspaceId {
			continue
		}
		if err := a.store.RemoveBlob(ref); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		delete(a.liveBlobs, ref)
	}
	return firstErr
}

func (a *RequestApi) cancelWorkspace(ctx context.Context, workspaceId string) error {
	if err := a.operations.cancelScope(ctx, workspaceId); err != nil {
		return err
	}
	return a.releaseWorkspaceBlobs(workspaceId)
}

func (a *RequestApi) resumeWorkspace(workspaceId string) {
	a.operations.resumeScope(workspaceId)
}

// CancelRequest 取消进行中的请求；未知/已完成的 sendId 为 no-op
func (a *RequestApi) CancelRequest(sendId string) error {
	a.operations.cancel(sendId)
	return nil
}

func (a *RequestApi) shutdown(ctx context.Context) error {
	if err := a.operations.shutdown(ctx); err != nil {
		return err
	}
	return a.releaseWorkspaceBlobs("")
}
