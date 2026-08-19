package binding

import (
	"errors"
	"fmt"
	"sync"

	"apirequest/backend/httpengine"
	"apirequest/backend/model"
	"apirequest/backend/platform"
	"apirequest/backend/secrets"
	"apirequest/backend/storage"
)

var (
	proxySettingKeys = []string{"proxy.mode", "proxy.url", "proxy.username", "proxy.password"}
	tlsSettingKeys   = []string{"tls.caCertPath", "tls.clientCertPath", "tls.clientKeyPath"}
)

// NetworkStatus describes the effective runtime network configuration without
// exposing proxy credentials or certificate contents.
type NetworkStatus struct {
	ProxyMode    string `json:"proxyMode"`
	ProxySource  string `json:"proxySource"`
	ProxyWarning string `json:"proxyWarning,omitempty"`
	TLSActive    bool   `json:"tlsActive"`
	TLSWarning   string `json:"tlsWarning,omitempty"`
}

// SettingsApi owns persisted application settings and their runtime effects.
type SettingsApi struct {
	store  *storage.Store
	engine *httpengine.Engine

	statusMu sync.RWMutex
	status   NetworkStatus
}

// NewSettingsApi restores saved proxy and TLS settings and retains recoverable
// startup diagnostics for the settings UI.
func NewSettingsApi(store *storage.Store, engine *httpengine.Engine) *SettingsApi {
	a := &SettingsApi{store: store, engine: engine}
	a.restoreProxy()
	a.restoreTLS()
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
	if err := a.store.MigrateAuditSecrets(); err != nil {
		return a.store.Vault().Status(), model.WrapError(model.KindStorage, err)
	}
	a.restoreProxy()
	return a.store.Vault().Status(), nil
}

// LockVault promotes runtime fallback references before clearing the encrypted
// file key. Proxy credentials are reloaded so locked file secrets leave memory.
func (a *SettingsApi) LockVault() (secrets.Status, error) {
	if err := a.store.MigrateSecrets(); err != nil {
		return a.store.Vault().Status(), model.WrapError(model.KindStorage, err)
	}
	a.store.Vault().Lock()
	a.restoreProxy()
	return a.store.Vault().Status(), nil
}

// ProxySettings is safe to return to the UI: Password is accepted on writes
// but always empty on reads; PasswordSet communicates persisted state.
type ProxySettings struct {
	Mode          string `json:"mode"` // system | manual | none
	Url           string `json:"url,omitempty"`
	Username      string `json:"username,omitempty"`
	Password      string `json:"password,omitempty"`
	PasswordSet   bool   `json:"passwordSet,omitempty"`
	ClearPassword bool   `json:"clearPassword,omitempty"`
}

type loadedProxySettings struct {
	public            ProxySettings
	password          string
	legacyCredentials bool
}

func normalizeProxyInput(settings ProxySettings) (ProxySettings, error) {
	if settings.Mode == "" {
		settings.Mode = "system"
	}
	if settings.Mode != "system" && settings.Mode != "manual" && settings.Mode != "none" {
		return ProxySettings{}, model.NewError(model.KindValidation, "invalid proxy mode: "+settings.Mode)
	}
	if settings.Url != "" {
		cleanURL, username, password, passwordSet, splitErr := platform.SplitProxyURLCredentials(settings.Url)
		if splitErr != nil {
			if settings.Mode == "manual" {
				return ProxySettings{}, model.WrapError(model.KindValidation, splitErr)
			}
			// A stale manual endpoint is irrelevant in system/direct mode. Do not
			// let malformed, unused input prevent the selected mode from working.
			settings.Url = ""
		} else {
			settings.Url = cleanURL
			if settings.Username == "" {
				settings.Username = username
			}
			if settings.Password == "" && passwordSet {
				settings.Password = password
			}
			settings.PasswordSet = settings.PasswordSet || passwordSet || settings.Password != ""
		}
	}
	if settings.Mode == "manual" {
		passwordSet := settings.PasswordSet || settings.Password != ""
		if _, err := platform.ProxyURLWithCredentials(settings.Url, settings.Username, settings.Password, passwordSet); err != nil {
			return ProxySettings{}, model.WrapError(model.KindValidation, err)
		}
	}
	return settings, nil
}

func (a *SettingsApi) loadProxySettings(resolvePassword bool) (loadedProxySettings, error) {
	values, err := a.store.GetSettings(proxySettingKeys)
	if err != nil {
		return loadedProxySettings{}, err
	}
	settings := ProxySettings{
		Mode:     values["proxy.mode"],
		Url:      values["proxy.url"],
		Username: values["proxy.username"],
	}
	if settings.Mode == "" {
		settings.Mode = "system"
	}
	cleanURL, inlineUsername, inlinePassword, inlinePasswordSet, splitErr := platform.SplitProxyURLCredentials(settings.Url)
	if splitErr != nil {
		if settings.Mode == "manual" {
			return loadedProxySettings{}, splitErr
		}
		cleanURL, inlineUsername, inlinePassword, inlinePasswordSet = settings.Url, "", "", false
	}
	legacyCredentials := inlineUsername != "" || inlinePasswordSet
	settings.Url = cleanURL
	if settings.Username == "" {
		settings.Username = inlineUsername
	}
	passwordRef := values["proxy.password"]
	settings.PasswordSet = passwordRef != "" || inlinePasswordSet
	loaded := loadedProxySettings{public: settings, legacyCredentials: legacyCredentials}
	if !resolvePassword {
		return loaded, nil
	}
	if passwordRef != "" {
		loaded.password, err = a.store.Vault().Resolve(passwordRef)
		if err != nil {
			return loadedProxySettings{}, fmt.Errorf("resolve proxy password: %w", err)
		}
	} else if inlinePasswordSet {
		loaded.password = inlinePassword
	}
	return loaded, nil
}

func (a *SettingsApi) persistProxySettings(settings ProxySettings) error {
	settings, err := normalizeProxyInput(settings)
	if err != nil {
		return err
	}
	return a.store.UpdateSecretSettings(
		proxySettingKeys,
		func(current map[string]string, writer secrets.SecretWriter) (map[string]string, error) {
			_, _, legacyPassword, legacyPasswordSet, splitErr := platform.SplitProxyURLCredentials(current["proxy.url"])
			if splitErr != nil {
				// The endpoint may be stale or malformed from an older build. It is
				// safe to discard its embedded credential when the new mode is being
				// persisted; manual mode validation already rejects the new input.
				legacyPassword, legacyPasswordSet = "", false
			}
			passwordRef := current["proxy.password"]
			password := settings.Password
			if password == "" && passwordRef == "" && legacyPasswordSet {
				password = legacyPassword
			}

			switch {
			case settings.ClearPassword:
				if secrets.IsRef(passwordRef) {
					if err := writer.Delete(passwordRef); err != nil && !errors.Is(err, secrets.ErrNotFound) {
						return nil, err
					}
				}
				passwordRef = ""
			case password != "":
				nextRef, err := writer.Put("setting/proxy/password", password)
				if err != nil {
					return nil, err
				}
				if secrets.IsRef(passwordRef) && passwordRef != nextRef {
					if err := writer.Delete(passwordRef); err != nil && !errors.Is(err, secrets.ErrNotFound) {
						return nil, err
					}
				}
				passwordRef = nextRef
			case settings.PasswordSet && passwordRef == "" && !legacyPasswordSet:
				return nil, errors.New("saved proxy password is unavailable")
			}

			if settings.Mode == "manual" && passwordRef != "" && settings.Username == "" {
				return nil, errors.New("proxy username is required when a password is set")
			}
			return map[string]string{
				"proxy.mode":     settings.Mode,
				"proxy.url":      settings.Url,
				"proxy.username": settings.Username,
				"proxy.password": passwordRef,
			}, nil
		},
	)
}

// validateProxyRuntime checks the final manual endpoint before any setting or
// Vault mutation. This keeps a failed validation from leaving persistence and
// the active Engine out of sync.
func (a *SettingsApi) validateProxyRuntime(settings ProxySettings) error {
	if settings.Mode != "manual" {
		return nil
	}
	password := settings.Password
	passwordSet := settings.PasswordSet || password != ""
	if password == "" && passwordSet && !settings.ClearPassword {
		loaded, err := a.loadProxySettings(true)
		if err != nil {
			return err
		}
		password = loaded.password
	}
	proxyURL, err := platform.ProxyURLWithCredentials(settings.Url, settings.Username, password, passwordSet && !settings.ClearPassword)
	if err != nil {
		return err
	}
	_, err = platform.ManualProxyFunc(proxyURL)
	return err
}

func (a *SettingsApi) rollbackProxySettings(previous map[string]string, previousPassword string, passwordAvailable bool) error {
	return a.store.UpdateSecretSettings(
		proxySettingKeys,
		func(current map[string]string, writer secrets.SecretWriter) (map[string]string, error) {
			previousRef := previous["proxy.password"]
			restoredRef := previousRef
			if previousRef != "" && passwordAvailable {
				ref, err := writer.Put("setting/proxy/password", previousPassword)
				if err != nil {
					return nil, err
				}
				restoredRef = ref
			}
			currentRef := current["proxy.password"]
			if secrets.IsRef(currentRef) && currentRef != restoredRef {
				if err := writer.Delete(currentRef); err != nil && !errors.Is(err, secrets.ErrNotFound) {
					return nil, err
				}
			}
			return map[string]string{
				"proxy.mode":     previous["proxy.mode"],
				"proxy.url":      previous["proxy.url"],
				"proxy.username": previous["proxy.username"],
				"proxy.password": restoredRef,
			}, nil
		},
	)
}

func (a *SettingsApi) configureProxy(settings loadedProxySettings) (platform.ProxyConfig, error) {
	proxyURL := settings.public.Url
	if settings.public.Mode == "manual" {
		var err error
		proxyURL, err = platform.ProxyURLWithCredentials(
			settings.public.Url,
			settings.public.Username,
			settings.password,
			settings.public.PasswordSet,
		)
		if err != nil {
			return platform.ProxyConfig{}, err
		}
	}
	return a.engine.ConfigureProxy(settings.public.Mode, proxyURL)
}

func (a *SettingsApi) restoreProxy() {
	loaded, err := a.loadProxySettings(true)
	if err != nil {
		_, _ = a.engine.ConfigureProxy("none", "")
		a.setProxyStatus("", "none", err.Error())
		return
	}
	if loaded.legacyCredentials {
		migration := loaded.public
		migration.Password = loaded.password
		if err := a.persistProxySettings(migration); err != nil {
			_, _ = a.engine.ConfigureProxy("none", "")
			a.setProxyStatus(loaded.public.Mode, "none", "migrate legacy proxy credentials: "+err.Error())
			return
		}
		loaded, err = a.loadProxySettings(true)
		if err != nil {
			_, _ = a.engine.ConfigureProxy("none", "")
			a.setProxyStatus("", "none", err.Error())
			return
		}
	}
	config, err := a.configureProxy(loaded)
	if err != nil {
		_, _ = a.engine.ConfigureProxy("none", "")
		a.setProxyStatus(loaded.public.Mode, "none", err.Error())
		return
	}
	a.setProxyStatus(loaded.public.Mode, config.Source, config.Warning)
}

// GetProxySettings reads proxy metadata without resolving its password.
func (a *SettingsApi) GetProxySettings() (ProxySettings, error) {
	loaded, err := a.loadProxySettings(false)
	if err != nil {
		return ProxySettings{}, model.WrapError(model.KindStorage, err)
	}
	return loaded.public, nil
}

// SetProxySettings persists proxy metadata and Vault credentials atomically,
// then applies the already-validated result to the runtime Engine.
func (a *SettingsApi) SetProxySettings(settings ProxySettings) error {
	previous, err := a.store.GetSettings(proxySettingKeys)
	if err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	previousPassword := ""
	previousPasswordAvailable := false
	if previousRef := previous["proxy.password"]; previousRef != "" {
		if resolved, resolveErr := a.store.Vault().Resolve(previousRef); resolveErr == nil {
			previousPassword = resolved
			previousPasswordAvailable = true
		}
	}
	settings, err = normalizeProxyInput(settings)
	if err != nil {
		return err
	}
	if err := a.validateProxyRuntime(settings); err != nil {
		return model.WrapError(model.KindValidation, err)
	}
	if err := a.persistProxySettings(settings); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	loaded, err := a.loadProxySettings(true)
	if err != nil {
		rollbackErr := a.rollbackProxySettings(previous, previousPassword, previousPasswordAvailable)
		if rollbackErr != nil {
			return model.WrapError(model.KindStorage, errors.Join(err, fmt.Errorf("rollback proxy settings: %w", rollbackErr)))
		}
		return model.WrapError(model.KindStorage, err)
	}
	config, err := a.configureProxy(loaded)
	if err != nil {
		rollbackErr := a.rollbackProxySettings(previous, previousPassword, previousPasswordAvailable)
		if rollbackErr != nil {
			return model.WrapError(model.KindValidation, errors.Join(err, fmt.Errorf("rollback proxy settings: %w", rollbackErr)))
		}
		return model.WrapError(model.KindValidation, err)
	}
	a.setProxyStatus(loaded.public.Mode, config.Source, config.Warning)
	return nil
}

func (a *SettingsApi) loadTLS() (httpengine.TLSSettings, error) {
	values, err := a.store.GetSettings(tlsSettingKeys)
	if err != nil {
		return httpengine.TLSSettings{}, err
	}
	return httpengine.TLSSettings{
		CaCertPath:     values["tls.caCertPath"],
		ClientCertPath: values["tls.clientCertPath"],
		ClientKeyPath:  values["tls.clientKeyPath"],
	}, nil
}

func tlsConfigured(settings httpengine.TLSSettings) bool {
	return settings.CaCertPath != "" || settings.ClientCertPath != "" || settings.ClientKeyPath != ""
}

func (a *SettingsApi) restoreTLS() {
	settings, err := a.loadTLS()
	if err != nil {
		a.setTLSStatus(false, err.Error())
		return
	}
	config, err := httpengine.PrepareTLS(settings)
	if err != nil {
		a.setTLSStatus(false, err.Error())
		return
	}
	a.engine.ApplyTLSConfig(config)
	a.setTLSStatus(tlsConfigured(settings), "")
}

// GetTLSSettings reads the complete TLS settings snapshot.
func (a *SettingsApi) GetTLSSettings() (httpengine.TLSSettings, error) {
	settings, err := a.loadTLS()
	if err != nil {
		return httpengine.TLSSettings{}, model.WrapError(model.KindStorage, err)
	}
	return settings, nil
}

// SetTLSSettings validates material first, commits all paths atomically, and
// only then swaps the runtime TLS configuration.
func (a *SettingsApi) SetTLSSettings(settings httpengine.TLSSettings) error {
	config, err := httpengine.PrepareTLS(settings)
	if err != nil {
		return err
	}
	if err := a.store.SetSettings(map[string]string{
		"tls.caCertPath":     settings.CaCertPath,
		"tls.clientCertPath": settings.ClientCertPath,
		"tls.clientKeyPath":  settings.ClientKeyPath,
	}); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	a.engine.ApplyTLSConfig(config)
	a.setTLSStatus(tlsConfigured(settings), "")
	return nil
}

// GetNetworkStatus returns the effective runtime source and startup warnings.
func (a *SettingsApi) GetNetworkStatus() NetworkStatus {
	a.statusMu.RLock()
	defer a.statusMu.RUnlock()
	return a.status
}

// RefreshSystemProxy re-reads native/environment settings without restarting.
func (a *SettingsApi) RefreshSystemProxy() (NetworkStatus, error) {
	settings, err := a.GetProxySettings()
	if err != nil {
		return a.GetNetworkStatus(), err
	}
	if settings.Mode != "system" {
		return a.GetNetworkStatus(), model.NewError(model.KindValidation, "system proxy mode is not active")
	}
	config, err := a.engine.ConfigureProxy("system", "")
	if err != nil {
		return a.GetNetworkStatus(), err
	}
	a.setProxyStatus("system", config.Source, config.Warning)
	return a.GetNetworkStatus(), nil
}

func (a *SettingsApi) setProxyStatus(mode, source, warning string) {
	a.statusMu.Lock()
	a.status.ProxyMode = mode
	a.status.ProxySource = source
	a.status.ProxyWarning = warning
	a.statusMu.Unlock()
}

func (a *SettingsApi) setTLSStatus(active bool, warning string) {
	a.statusMu.Lock()
	a.status.TLSActive = active
	a.status.TLSWarning = warning
	a.statusMu.Unlock()
}
