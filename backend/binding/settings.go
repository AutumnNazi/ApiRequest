package binding

import (
	"apirequest/backend/httpengine"
	"apirequest/backend/model"
	"apirequest/backend/storage"
)

// SettingsApi 应用设置域（setting 表 KV + 代理注入）
type SettingsApi struct {
	store  *storage.Store
	engine *httpengine.Engine
}

// NewSettingsApi 构造；启动时恢复已保存的代理设置
func NewSettingsApi(store *storage.Store, engine *httpengine.Engine) *SettingsApi {
	a := &SettingsApi{store: store, engine: engine}
	mode, _ := store.GetSetting("proxy.mode")
	proxyUrl, _ := store.GetSetting("proxy.url")
	if mode != "" {
		engine.SetProxy(mode, proxyUrl)
	}
	return a
}

// ProxySettings 代理配置
type ProxySettings struct {
	Mode string `json:"mode"` // system | manual | none
	Url  string `json:"url,omitempty"`
}

// GetProxySettings 读代理配置
func (a *SettingsApi) GetProxySettings() (ProxySettings, error) {
	mode, err := a.store.GetSetting("proxy.mode")
	if err != nil {
		return ProxySettings{}, model.WrapError(model.KindStorage, err)
	}
	if mode == "" {
		mode = "system"
	}
	u, _ := a.store.GetSetting("proxy.url")
	return ProxySettings{Mode: mode, Url: u}, nil
}

// SetProxySettings 保存并立即生效
func (a *SettingsApi) SetProxySettings(p ProxySettings) error {
	if p.Mode != "system" && p.Mode != "manual" && p.Mode != "none" {
		return model.NewError(model.KindValidation, "invalid proxy mode: "+p.Mode)
	}
	if err := a.engine.SetProxy(p.Mode, p.Url); err != nil {
		return err
	}
	if err := a.store.SetSetting("proxy.mode", p.Mode); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	if err := a.store.SetSetting("proxy.url", p.Url); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	return nil
}
