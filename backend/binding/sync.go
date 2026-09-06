package binding

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	wailsrt "github.com/wailsapp/wails/v2/pkg/runtime"

	"apirequest/backend/httpengine"
	"apirequest/backend/model"
	"apirequest/backend/secrets"
	"apirequest/backend/storage"
	appsync "apirequest/backend/sync"
)

// SyncApi WebDAV 同步域（docs/sync.md）。密码仅以 Vault 引用持久化。
type SyncApi struct {
	store  *storage.Store
	engine *httpengine.Engine
	// ctx 的写入（startup）与 SyncNow 中的读取无同步，用 atomic 消除数据竞争
	// （与 OAuth2Api 同一处理）
	ctx atomic.Value // context.Context
	// operations 与 Request/Runner 共享的注册表：按 workspaceId 注册 scope，
	// 使同步可被 DeleteWorkspace 屏蔽、可随应用退出取消
	operations *operationRegistry
	// 自动同步调度器：stop 管道存在即运行中（StartAutoSync 幂等）
	autoMu   sync.Mutex
	autoStop chan struct{}
}

// NewSyncApi 构造
func NewSyncApi(store *storage.Store, engine *httpengine.Engine, operations *operationRegistry) *SyncApi {
	if operations == nil {
		operations = newOperationRegistry()
	}
	return &SyncApi{store: store, engine: engine, operations: operations}
}

func (a *SyncApi) startup(ctx context.Context) { a.ctx.Store(ctx) }

func (a *SyncApi) currentCtx() context.Context {
	if ctx, ok := a.ctx.Load().(context.Context); ok && ctx != nil {
		return ctx
	}
	return context.Background()
}

// GetSyncConfig 读可公开配置。密码不跨 Wails 边界，只返回是否已设置。
func (a *SyncApi) GetSyncConfig() (appsync.DavConfig, error) {
	return a.loadSyncConfig(false)
}

func (a *SyncApi) loadSyncConfig(resolvePassword bool) (appsync.DavConfig, error) {
	raw, err := a.store.GetSetting("sync.webdav")
	if err != nil {
		return appsync.DavConfig{}, model.WrapError(model.KindStorage, err)
	}
	var cfg appsync.DavConfig
	if raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			return appsync.DavConfig{}, model.WrapError(model.KindStorage, err)
		}
	}
	if cfg.Password != "" {
		cfg.PasswordSet = true
		if resolvePassword {
			resolved, err := a.store.Vault().Resolve(cfg.Password)
			if err != nil {
				return appsync.DavConfig{}, model.WrapError(model.KindStorage, err)
			}
			cfg.Password = resolved
		} else {
			cfg.Password = ""
		}
	}
	cfg.ClearPassword = false
	return cfg, nil
}

// SetSyncConfig 保存同步配置
func (a *SyncApi) SetSyncConfig(cfg appsync.DavConfig) error {
	err := a.store.UpdateSecretSetting("sync.webdav", func(existingRaw string, writer secrets.SecretWriter) (string, error) {
		var existing appsync.DavConfig
		if existingRaw != "" {
			if err := json.Unmarshal([]byte(existingRaw), &existing); err != nil {
				return "", err
			}
		}
		oldPasswordRef := existing.Password
		switch {
		case cfg.ClearPassword:
			cfg.Password = ""
		case cfg.Password != "":
			ref, err := writer.PutPlaintext("setting/sync.webdav/password", cfg.Password)
			if err != nil {
				return "", err
			}
			cfg.Password = ref
		default:
			cfg.Password = oldPasswordRef
		}
		cfg.PasswordSet = cfg.Password != ""
		cfg.ClearPassword = false
		data, err := json.Marshal(cfg)
		if err != nil {
			return "", model.WrapError(model.KindValidation, err)
		}
		if secrets.IsRef(oldPasswordRef) && oldPasswordRef != cfg.Password {
			if err := writer.Delete(oldPasswordRef); err != nil {
				return "", err
			}
		}
		return string(data), nil
	})
	if err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	return nil
}

// SyncNow 立即对指定工作区执行一次双向同步
func (a *SyncApi) SyncNow(workspaceId string) (*appsync.Report, error) {
	if workspaceId == "" {
		return nil, model.NewError(model.KindValidation, "workspaceId is required")
	}
	cfg, err := a.loadSyncConfig(true)
	if err != nil {
		return nil, err
	}
	if cfg.Url == "" {
		return nil, model.NewError(model.KindValidation, "WebDAV not configured; set it in Settings first")
	}
	// 去重而非排队：同一 workspace 已有同步在跑时直接失败返回，让前端立刻拿到
	// 明确错误。若用互斥锁，第二次点击会干等第一轮跑完再完整跑一轮无意义的
	// GET→merge→PUT，期间前端 Promise 一直挂起
	ctx, finish, err := a.operations.begin(a.currentCtx(), "sync:"+workspaceId, workspaceId)
	if err != nil {
		// 关停（应用退出）是生命周期事件，报 Validation 会把它说成用户输入问题
		kind := model.KindValidation
		if IsRegistryClosing(err) {
			kind = model.KindNetwork
		}
		return nil, model.NewError(kind, fmt.Sprintf("start sync: %v", err))
	}
	defer finish()
	// 传 0（无 Client.Timeout）：超时由 dav 层按 body 大小逐请求派生 ctx 控制。
	// 设 Client.Timeout 会成为覆盖 body 传输全程的硬上限，先于 dav 的放大逻辑掐断大快照
	return appsync.SyncWithClientCtx(ctx, a.store, workspaceId, cfg, a.engine.NewHTTPClient(0))
}

// ── 自动定时同步 ──

// StartAutoSync 启动轮询调度器并返回停止函数（幂等：重复调用返回同一停止语义）。
// tick 粒度只决定"检查是否到点"的频率，实际触发间隔由每工作区的 intervalMinutes
// 与持久化的上次完成时间共同决定。生产用 30s tick；测试可注入更小值。
func (a *SyncApi) StartAutoSync(tick time.Duration) (stop func()) {
	a.autoMu.Lock()
	defer a.autoMu.Unlock()
	if a.autoStop != nil {
		existing := a.autoStop
		return func() {
			existing <- struct{}{}
		}
	}
	stopCh := make(chan struct{})
	a.autoStop = stopCh
	go func() {
		ticker := time.NewTicker(tick)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				if !a.runDueAutoSyncs() {
					return
				}
			}
		}
	}()
	return func() {
		// 非阻塞发送：调度器已退出时不再等待
		select {
		case stopCh <- struct{}{}:
		default:
		}
		a.autoMu.Lock()
		if a.autoStop == stopCh {
			a.autoStop = nil
		}
		a.autoMu.Unlock()
	}
}

// runDueAutoSyncs 对每个工作区检查到期与否并触发。返回 false 表示注册表已关停
// （应用退出），调度器应当退出。
func (a *SyncApi) runDueAutoSyncs() bool {
	cfg, err := a.loadSyncConfig(false)
	if err != nil || cfg.IntervalMinutes <= 0 || cfg.Url == "" {
		return true
	}
	workspaces, err := a.store.ListWorkspaces()
	if err != nil {
		return true
	}
	for _, ws := range workspaces {
		if a.notDueAutoSync(ws.Id, cfg.IntervalMinutes) {
			continue
		}
		if err := a.markAutoSyncDone(ws.Id); err != nil {
			continue
		}
		// 复用 SyncNow 的全部守卫：去重（手动同步在跑则跳过）、配置校验、关停
		report, err := a.SyncNow(ws.Id)
		if err != nil {
			if IsRegistryClosing(err) {
				return false
			}
			// 自动同步静默失败：结果页由下次手动同步呈现，这里不惊扰用户
			continue
		}
		a.emitAutoSyncResult(ws.Id, report)
	}
	return true
}

// notDueAutoSync 返回 true 表示还没到下一次触发时间。
// 从未同步过的工作区立即到期（首次启动即拉取远端）。
func (a *SyncApi) notDueAutoSync(workspaceId string, intervalMinutes int) bool {
	if intervalMinutes <= 0 {
		return true // 间隔关闭 = 永不到期
	}
	raw, err := a.store.GetSetting("sync.auto." + workspaceId)
	if err != nil || raw == "" {
		return false
	}
	var lastRun int64
	if err := json.Unmarshal([]byte(raw), &lastRun); err != nil {
		return false
	}
	next := lastRun + int64(intervalMinutes)*int64(time.Minute)
	return time.Now().UnixMilli() < next
}

// markAutoSyncDone 记录触发时间（成功或失败都记录）：失败的间隔内不重试，避免
// 对不可用 WebDAV 每 30s 重试风暴。重试节奏与成功同步一致（下一整间隔）。
func (a *SyncApi) markAutoSyncDone(workspaceId string) error {
	data, err := json.Marshal(time.Now().UnixMilli())
	if err != nil {
		return err
	}
	return a.store.SetSetting("sync.auto."+workspaceId, string(data))
}

// emitAutoSyncResult 推送 sync:auto 事件（前端可订阅做轻提示）
func (a *SyncApi) emitAutoSyncResult(workspaceId string, report *appsync.Report) {
	if ctx, ok := a.ctx.Load().(context.Context); ok && ctx != nil {
		wailsrt.EventsEmit(ctx, "sync:auto", map[string]any{
			"workspaceId": workspaceId,
			"pushed":      report.Pushed,
			"pulled":      report.Pulled,
			"deleted":     report.Deleted,
			"syncedAt":    report.SyncedAt,
			"conflicts":   len(report.Conflicts),
		})
	}
}

// ── 远端工作区发现与导入（docs/sync.md）──

// ListRemoteWorkspaces 列出远端 WebDAV 目录下的工作区快照
func (a *SyncApi) ListRemoteWorkspaces() ([]appsync.RemoteWorkspaceInfo, error) {
	cfg, err := a.loadSyncConfig(true)
	if err != nil {
		return nil, err
	}
	if cfg.Url == "" {
		return nil, model.NewError(model.KindValidation, "WebDAV not configured; set it in Settings first")
	}
	return appsync.DiscoverRemoteWorkspaces(cfg)
}

// ImportRemoteWorkspace 把远端快照导入为本地工作区（id 沿用远端，后续同步命中同一路径）
func (a *SyncApi) ImportRemoteWorkspace(workspaceId string) (*appsync.Report, error) {
	if workspaceId == "" {
		return nil, model.NewError(model.KindValidation, "workspaceId is required")
	}
	cfg, err := a.loadSyncConfig(true)
	if err != nil {
		return nil, err
	}
	if cfg.Url == "" {
		return nil, model.NewError(model.KindValidation, "WebDAV not configured; set it in Settings first")
	}
	return appsync.ImportWorkspace(a.store, cfg, workspaceId)
}
