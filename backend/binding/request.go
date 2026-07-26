// Package binding 是 Wails API 边界：把 core 能力以导出方法暴露给前端，
// 统一 AppError 序列化与事件推送（docs/api-contract.md）。
package binding

import (
	"context"
	"sync"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"

	"apirequest/backend/httpengine"
	"apirequest/backend/model"
	"apirequest/backend/storage"
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
func Startup(ctx context.Context, apis ...*RequestApi) {
	for _, a := range apis {
		a.ctx = ctx
	}
}

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

// SendRequest 执行请求并落历史。sendId 由前端生成，用于关联进度事件与取消。
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

	a.emitProgress(sendId, "sending")
	res, err := a.engine.Send(ctx, req)
	a.emitProgress(sendId, "done")
	if err != nil {
		return res, model.WrapError(model.KindNetwork, err)
	}

	// 落历史（失败不阻断响应返回，仅附带告知）
	histId, herr := a.store.InsertHistory(model.HistoryItem{
		WorkspaceId: sendCtx.WorkspaceId,
		RequestSnap: req,
		Status:      res.Status,
		DurationMs:  int64(res.Timing.TotalMs),
		SizeBytes:   res.SizeBytes,
		Timing:      res.Timing,
		RespHeaders: res.Headers,
		BodyInline:  res.Body.Text,
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
