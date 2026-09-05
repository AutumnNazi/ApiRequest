package binding

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"

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
