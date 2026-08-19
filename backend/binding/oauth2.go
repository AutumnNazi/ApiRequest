package binding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"apirequest/backend/auth"
	"apirequest/backend/platform"
	"apirequest/backend/secrets"
	"apirequest/backend/storage"
)

const oauthTokenSettingPrefix = "oauth.token."

type oauthTokenStore struct{ store *storage.Store }

func newOAuthTokenStore(store *storage.Store) *oauthTokenStore {
	return &oauthTokenStore{store: store}
}

func oauthTokenSettingKey(fingerprint string) string {
	return oauthTokenSettingPrefix + fingerprint
}

func (s *oauthTokenStore) Load(fingerprint string) (*auth.Token, error) {
	ref, err := s.store.GetSetting(oauthTokenSettingKey(fingerprint))
	if err != nil || ref == "" {
		return nil, err
	}
	if !secrets.IsRef(ref) {
		return nil, errors.New("OAuth token setting does not contain a Vault reference")
	}
	raw, err := s.store.Vault().Resolve(ref)
	if err != nil {
		return nil, fmt.Errorf("resolve OAuth token: %w", err)
	}
	var token auth.Token
	if err := json.Unmarshal([]byte(raw), &token); err != nil {
		return nil, fmt.Errorf("decode OAuth token: %w", err)
	}
	if token.AccessToken == "" {
		return nil, errors.New("stored OAuth token has no access token")
	}
	return &token, nil
}

func (s *oauthTokenStore) Save(fingerprint string, token *auth.Token) error {
	if token == nil || token.AccessToken == "" {
		return errors.New("OAuth access token is required")
	}
	raw, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("encode OAuth token: %w", err)
	}
	settingKey := oauthTokenSettingKey(fingerprint)
	return s.store.UpdateSecretSetting(settingKey, func(existing string, writer secrets.SecretWriter) (string, error) {
		ref, err := writer.Put("setting/"+settingKey, string(raw))
		if err != nil {
			return "", err
		}
		if secrets.IsRef(existing) && existing != ref {
			if err := writer.Delete(existing); err != nil && !errors.Is(err, secrets.ErrNotFound) {
				return "", err
			}
		}
		return ref, nil
	})
}

func (s *oauthTokenStore) Delete(fingerprint string) error {
	return s.store.UpdateSecretSetting(
		oauthTokenSettingKey(fingerprint),
		func(existing string, writer secrets.SecretWriter) (string, error) {
			if secrets.IsRef(existing) {
				if err := writer.Delete(existing); err != nil && !errors.Is(err, secrets.ErrNotFound) {
					return "", err
				}
			}
			return "", nil
		},
	)
}

// OAuth2Api OAuth 2.0 token 获取域
type OAuth2Api struct {
	ctx     context.Context
	manager *auth.TokenManager
}

// NewOAuth2Api 构造。浏览器打开延迟到调用时（ctx 由 startup 注入）
func NewOAuth2Api(store *storage.Store, clients ...*http.Client) *OAuth2Api {
	api := &OAuth2Api{}
	client := http.DefaultClient
	if len(clients) > 0 && clients[0] != nil {
		client = clients[0]
	}
	api.manager = auth.NewTokenManagerWithStore(func(url string) error {
		return platform.OpenURL(api.ctx, url)
	}, newOAuthTokenStore(store), client)
	return api
}

func (a *OAuth2Api) startup(ctx context.Context) { a.ctx = ctx }

// GetOAuth2Token 按 auth 参数获取 token（缓存/刷新/完整流程自动选择）。
// 授权码模式会拉起系统浏览器等待回调（最长 120s）。
func (a *OAuth2Api) GetOAuth2Token(params map[string]string) (*auth.Token, error) {
	return a.manager.GetToken(context.Background(), params)
}

// ClearOAuth2Token 清除该配置的缓存 token
func (a *OAuth2Api) ClearOAuth2Token(params map[string]string) error {
	return a.manager.ClearToken(params)
}
