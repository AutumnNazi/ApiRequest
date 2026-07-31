package binding

import (
	"apirequest/backend/httpengine"
	"apirequest/backend/model"
	"apirequest/backend/secrets"
	"apirequest/backend/storage"
)

// SettingsApi 应用设置域（setting 表 KV + 代理注入）
type SettingsApi struct {
	store  *storage.Store
	engine *httpengine.Engine
}

// NewSettingsApi 构造；启动时恢复已保存的代理与 TLS 设置
func NewSettingsApi(store *storage.Store, engine *httpengine.Engine) *SettingsApi {
	a := &SettingsApi{store: store, engine: engine}
	mode, _ := store.GetSetting("proxy.mode")
	proxyUrl, _ := store.GetSetting("proxy.url")
	if mode != "" {
		engine.SetProxy(mode, proxyUrl)
	}
	tlsSettings := a.loadTLS()
	if tlsSettings.CaCertPath != "" || tlsSettings.ClientCertPath != "" {
		engine.SetTLS(tlsSettings) // 失败静默：文件可能已被移动，用户可在设置里重新配置
	}
	return a
}

// GetVaultStatus reports which Secret Vault Adapter is available.
func (a *SettingsApi) GetVaultStatus() secrets.Status {
	return a.store.Vault().Status()
}

// UnlockVault unlocks the encrypted-file fallback and migrates legacy plaintext values.
func (a *SettingsApi) UnlockVault(password string) (secrets.Status, error) {
	if err := a.store.Vault().Unlock(password); err != nil {
		return a.store.Vault().Status(), model.WrapError(model.KindValidation, err)
	}
	if err := a.store.MigrateSecrets(); err != nil {
		return a.store.Vault().Status(), model.WrapError(model.KindStorage, err)
	}
	return a.store.Vault().Status(), nil
}

// LockVault clears the encrypted-file key and decrypted cache from memory.
func (a *SettingsApi) LockVault() secrets.Status {
	a.store.Vault().Lock()
	return a.store.Vault().Status()
}

func (a *SettingsApi) loadTLS() httpengine.TLSSettings {
	ca, _ := a.store.GetSetting("tls.caCertPath")
	cert, _ := a.store.GetSetting("tls.clientCertPath")
	key, _ := a.store.GetSetting("tls.clientKeyPath")
	return httpengine.TLSSettings{CaCertPath: ca, ClientCertPath: cert, ClientKeyPath: key}
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

// GetTLSSettings 读 TLS 配置
func (a *SettingsApi) GetTLSSettings() (httpengine.TLSSettings, error) {
	return a.loadTLS(), nil
}

// SetTLSSettings 保存并立即生效（空配置 = 恢复默认）
func (a *SettingsApi) SetTLSSettings(s httpengine.TLSSettings) error {
	if err := a.engine.SetTLS(s); err != nil {
		return err
	}
	for k, v := range map[string]string{
		"tls.caCertPath":     s.CaCertPath,
		"tls.clientCertPath": s.ClientCertPath,
		"tls.clientKeyPath":  s.ClientKeyPath,
	} {
		if err := a.store.SetSetting(k, v); err != nil {
			return model.WrapError(model.KindStorage, err)
		}
	}
	return nil
}
