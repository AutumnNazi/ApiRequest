package binding

import (
	"encoding/json"

	"apirequest/backend/httpengine"
	"apirequest/backend/model"
	"apirequest/backend/storage"
	appsync "apirequest/backend/sync"
)

// SyncApi WebDAV 同步域（docs/sync.md）。
// 配置存 setting 表（密码含在内——本地库本身在用户机器上；
// 更强保护待 keychain 集成后迁移）。
type SyncApi struct {
	store  *storage.Store
	engine *httpengine.Engine
}

// NewSyncApi 构造
func NewSyncApi(store *storage.Store, engine *httpengine.Engine) *SyncApi {
	return &SyncApi{store: store, engine: engine}
}

// GetSyncConfig 读同步配置（密码原样返回供表单回显；仅本机使用）
func (a *SyncApi) GetSyncConfig() (appsync.DavConfig, error) {
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
	return cfg, nil
}

// SetSyncConfig 保存同步配置
func (a *SyncApi) SetSyncConfig(cfg appsync.DavConfig) error {
	b, err := json.Marshal(cfg)
	if err != nil {
		return model.WrapError(model.KindValidation, err)
	}
	if err := a.store.SetSetting("sync.webdav", string(b)); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	return nil
}

// SyncNow 立即对指定工作区执行一次双向同步
func (a *SyncApi) SyncNow(workspaceId string) (*appsync.Report, error) {
	if workspaceId == "" {
		return nil, model.NewError(model.KindValidation, "workspaceId is required")
	}
	cfg, err := a.GetSyncConfig()
	if err != nil {
		return nil, err
	}
	if cfg.Url == "" {
		return nil, model.NewError(model.KindValidation, "WebDAV not configured; set it in Settings first")
	}
	return appsync.Sync(a.store, workspaceId, cfg)
}
