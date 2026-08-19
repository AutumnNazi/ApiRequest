package binding

import (
	"testing"

	"apirequest/backend/auth"
	"apirequest/backend/secrets"
)

func TestOAuthTokenStoreUsesVaultReferences(t *testing.T) {
	store, adapter := openSettingsTestStore(t)
	tokens := newOAuthTokenStore(store)
	want := &auth.Token{AccessToken: "access-secret", RefreshToken: "refresh-secret", ExpiresAt: 12345}
	if err := tokens.Save("fingerprint", want); err != nil {
		t.Fatal(err)
	}
	raw, err := store.GetSetting("oauth.token.fingerprint")
	if err != nil {
		t.Fatal(err)
	}
	if !secrets.IsRef(raw) || raw == "access-secret" || len(adapter.values) != 1 {
		t.Fatalf("stored token reference = %q, Vault = %+v", raw, adapter.values)
	}
	got, err := tokens.Load("fingerprint")
	if err != nil || got == nil || got.AccessToken != want.AccessToken || got.RefreshToken != want.RefreshToken {
		t.Fatalf("loaded token = %+v, err = %v", got, err)
	}
	if err := tokens.Delete("fingerprint"); err != nil {
		t.Fatal(err)
	}
	if got, err := tokens.Load("fingerprint"); err != nil || got != nil {
		t.Fatalf("token after delete = %+v, err = %v", got, err)
	}
}
