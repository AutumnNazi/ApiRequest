package binding

import (
	"encoding/json"

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
}

// NewSyncApi 构造
func NewSyncApi(store *storage.Store, engine *httpengine.Engine) *SyncApi {
	return &SyncApi{store: store, engine: engine}
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
	return appsync.Sync(a.store, workspaceId, cfg)
}
