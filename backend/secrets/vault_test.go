package secrets

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"apirequest/backend/model"
)

type fakeKeyring struct {
	values      map[string]string
	unavailable bool
}

func (f *fakeKeyring) Set(_, account, value string) error {
	if f.unavailable {
		return errors.New("keyring unavailable")
	}
	if f.values == nil {
		f.values = map[string]string{}
	}
	f.values[account] = value
	return nil
}

func (f *fakeKeyring) Get(_, account string) (string, error) {
	if f.unavailable {
		return "", errors.New("keyring unavailable")
	}
	value, ok := f.values[account]
	if !ok {
		return "", ErrNotFound
	}
	return value, nil
}

func (f *fakeKeyring) Delete(_, account string) error {
	if f.unavailable {
		return errors.New("keyring unavailable")
	}
	if _, ok := f.values[account]; !ok {
		return ErrNotFound
	}
	delete(f.values, account)
	return nil
}

func TestKeyringRoundTripAndStableReference(t *testing.T) {
	adapter := &fakeKeyring{}
	vault := NewWithKeyring(t.TempDir(), adapter)
	if status := vault.Status(); status.Mode != "keyring" || !status.CanStore {
		t.Fatalf("status = %+v", status)
	}

	ref, err := vault.Put("node/n1/auth/token", "top-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ref, keyringRef) || strings.Contains(ref, "top-secret") {
		t.Fatalf("unsafe ref %q", ref)
	}
	refAgain, err := vault.Put("node/n1/auth/token", "rotated-secret")
	if err != nil || refAgain != ref {
		t.Fatalf("stable ref = %q, err = %v", refAgain, err)
	}
	value, err := vault.Resolve(ref)
	if err != nil || value != "rotated-secret" {
		t.Fatalf("resolved = %q, err = %v", value, err)
	}
	if got := vault.RedactString("token=rotated-secret"); got != "token=<redacted>" {
		t.Fatalf("redacted = %q", got)
	}
}

func TestWriteBatchRollbackRestoresAndDeletesEntries(t *testing.T) {
	adapter := &fakeKeyring{}
	vault := NewWithKeyring(t.TempDir(), adapter)
	oldRef, err := vault.Put("node/n1/auth/token", "old-secret")
	if err != nil {
		t.Fatal(err)
	}

	batch := vault.BeginWrite()
	updatedRef, err := batch.Put("node/n1/auth/token", "new-secret")
	if err != nil {
		t.Fatal(err)
	}
	newRef, err := batch.Put("node/n1/variable/extra", "extra-secret")
	if err != nil {
		t.Fatal(err)
	}
	if updatedRef != oldRef || newRef == oldRef {
		t.Fatalf("updated ref = %q, old = %q, new = %q", updatedRef, oldRef, newRef)
	}
	if err := batch.Delete(updatedRef); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Resolve(updatedRef); !errors.Is(err, ErrNotFound) {
		t.Fatalf("batch delete left entry accessible: %v", err)
	}
	if err := batch.Rollback(); err != nil {
		t.Fatal(err)
	}
	if value, err := vault.Resolve(oldRef); err != nil || value != "old-secret" {
		t.Fatalf("restored value = %q, err = %v", value, err)
	}
	if _, err := vault.Resolve(newRef); !errors.Is(err, ErrNotFound) {
		t.Fatalf("new entry survived rollback: %v", err)
	}
}

func TestEncryptedFileRoundTripWrongPasswordAndRestartLocked(t *testing.T) {
	dir := t.TempDir()
	vault := NewWithKeyring(dir, &fakeKeyring{unavailable: true})
	if _, err := vault.Put("node/n1/auth/password", "secret"); !errors.Is(err, ErrLocked) {
		t.Fatalf("put while locked error = %v", err)
	}
	if err := vault.Unlock("correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	ref, err := vault.Put("node/n1/auth/password", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ref, fileRef) {
		t.Fatalf("ref = %q", ref)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "secrets.vault"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret") {
		t.Fatal("encrypted file contains plaintext")
	}

	restarted := NewWithKeyring(dir, &fakeKeyring{unavailable: true})
	if status := restarted.Status(); status.Mode != "locked" || !status.FileExists {
		t.Fatalf("restart status = %+v", status)
	}
	if _, err := restarted.Resolve(ref); !errors.Is(err, ErrLocked) {
		t.Fatalf("resolve while locked error = %v", err)
	}
	if err := restarted.Unlock("wrong password"); err == nil {
		t.Fatal("wrong password unexpectedly unlocked vault")
	}
	if err := restarted.Unlock("correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	value, err := restarted.Resolve(ref)
	if err != nil || value != "secret" {
		t.Fatalf("resolved = %q, err = %v", value, err)
	}
}

func TestEncryptedFileBatchDeleteRollbackRestoresValue(t *testing.T) {
	vault := NewWithKeyring(t.TempDir(), &fakeKeyring{unavailable: true})
	if err := vault.Unlock("correct horse battery staple"); err != nil {
		t.Fatal(err)
	}
	ref, err := vault.Put("node/n1/auth/password", "old-secret")
	if err != nil {
		t.Fatal(err)
	}
	batch := vault.BeginWrite()
	if err := batch.Delete(ref); err != nil {
		t.Fatal(err)
	}
	if _, err := vault.Resolve(ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted file secret remains accessible: %v", err)
	}
	if err := batch.Rollback(); err != nil {
		t.Fatal(err)
	}
	if value, err := vault.Resolve(ref); err != nil || value != "old-secret" {
		t.Fatalf("restored file value = %q, err = %v", value, err)
	}
}

func TestEncryptedFileRejectsOversizedVault(t *testing.T) {
	dir := t.TempDir()
	file, err := os.Create(filepath.Join(dir, "secrets.vault"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxEncryptedVaultSize + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	vault := NewWithKeyring(dir, &fakeKeyring{unavailable: true})
	if err := vault.Unlock("correct horse battery staple"); err == nil {
		t.Fatal("oversized encrypted vault was loaded")
	}
}

func TestProtectResolveAndRedactDomainValues(t *testing.T) {
	vault := NewWithKeyring(t.TempDir(), &fakeKeyring{})
	auth := model.Auth{Type: "oauth2", Params: map[string]string{
		"clientId":     "public-client",
		"clientSecret": "client-secret",
		"access_token": "access-secret",
	}}
	protected, err := ProtectAuth(vault, auth, "node/n1")
	if err != nil {
		t.Fatal(err)
	}
	if protected.Params["clientId"] != "public-client" || !IsRef(protected.Params["clientSecret"]) || !IsRef(protected.Params["access_token"]) {
		t.Fatalf("protected auth = %+v", protected)
	}
	resolved, err := ResolveAuth(vault, protected)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Params["clientSecret"] != "client-secret" || resolved.Params["access_token"] != "access-secret" {
		t.Fatalf("resolved auth = %+v", resolved)
	}

	variables := []model.Variable{
		{Key: "region", Value: "cn-north-1", Type: "default", Enabled: true},
		{Key: "password", Value: "variable-secret", Type: "secret", Enabled: true},
	}
	protectedVars, err := ProtectVariables(vault, variables, "environment/e1")
	if err != nil {
		t.Fatal(err)
	}
	if protectedVars[0].Value != "cn-north-1" || !IsRef(protectedVars[1].Value) {
		t.Fatalf("protected vars = %+v", protectedVars)
	}
	resolvedVars, err := ResolveVariables(vault, protectedVars)
	if err != nil || resolvedVars[1].Value != "variable-secret" {
		t.Fatalf("resolved vars = %+v, err = %v", resolvedVars, err)
	}

	request := model.HttpRequest{Auth: auth}
	redacted := RedactRequest(request)
	if redacted.Auth.Params["clientSecret"] != redactedText || redacted.Auth.Params["access_token"] != redactedText {
		t.Fatalf("redacted request = %+v", redacted)
	}
	if redacted.Auth.Params["clientId"] != "public-client" {
		t.Fatalf("public auth parameter was redacted: %+v", redacted)
	}
}

func TestReferenceLikePublicValuesAreNotResolvedOrOwned(t *testing.T) {
	vault := NewWithKeyring(t.TempDir(), &fakeKeyring{unavailable: true})
	const literal = "secret://file/literal-user-value"
	auth := model.Auth{Type: "basic", Params: map[string]string{"username": literal}}
	resolvedAuth, err := ResolveAuth(vault, auth)
	if err != nil || resolvedAuth.Params["username"] != literal {
		t.Fatalf("public auth value was interpreted as a reference: %+v, %v", resolvedAuth, err)
	}
	variables := []model.Variable{{Key: "endpoint", Value: literal, Type: "default", Enabled: true}}
	resolvedVariables, err := ResolveVariables(vault, variables)
	if err != nil || resolvedVariables[0].Value != literal {
		t.Fatalf("default variable was interpreted as a reference: %+v, %v", resolvedVariables, err)
	}
	if refs := AuthReferences(auth); len(refs) != 0 {
		t.Fatalf("public auth value was claimed by Vault: %+v", refs)
	}
	if refs := VariableReferences(variables); len(refs) != 0 {
		t.Fatalf("default variable was claimed by Vault: %+v", refs)
	}
}

func TestRequestScopedRedactor(t *testing.T) {
	vault := NewWithKeyring(t.TempDir(), &fakeKeyring{})
	redactor := NewRedactor(vault, "abc123")
	got := redactor.String("Authorization: Bearer abc123")
	if got != "Authorization: Bearer <redacted>" {
		t.Fatalf("redacted = %q", got)
	}
}

func TestProtectVariablesKeepsDuplicateKeysIndependent(t *testing.T) {
	vault := NewWithKeyring(t.TempDir(), &fakeKeyring{})
	variables := []model.Variable{
		{Key: "token", Value: "first-secret", Type: "secret", Enabled: true},
		{Key: "token", Value: "second-secret", Type: "secret", Enabled: true},
	}
	protected, err := ProtectVariables(vault, variables, "environment/e1")
	if err != nil {
		t.Fatal(err)
	}
	if protected[0].Value == protected[1].Value {
		t.Fatalf("duplicate variables share ref %q", protected[0].Value)
	}
	resolved, err := ResolveVariables(vault, protected)
	if err != nil {
		t.Fatal(err)
	}
	if resolved[0].Value != "first-secret" || resolved[1].Value != "second-secret" {
		t.Fatalf("resolved duplicate variables = %+v", resolved)
	}

	resolved[0].Value = "rotated-first"
	protectedAgain, err := ProtectVariables(vault, resolved, "environment/e1")
	if err != nil {
		t.Fatal(err)
	}
	resolvedAgain, err := ResolveVariables(vault, protectedAgain)
	if err != nil {
		t.Fatal(err)
	}
	if resolvedAgain[0].Value != "rotated-first" || resolvedAgain[1].Value != "second-secret" {
		t.Fatalf("updating one duplicate changed another: %+v", resolvedAgain)
	}
}

func TestRestoreOmittedVariablesUsesDuplicateOccurrence(t *testing.T) {
	local := []model.Variable{
		{Key: "token", Value: "first-secret", Type: "secret", Enabled: true},
		{Key: "region", Value: "cn-north-1", Type: "default", Enabled: true},
		{Key: "token", Value: "second-secret", Type: "secret", Enabled: true},
	}
	target := OmitVariables(local)
	restored := RestoreOmittedVariables(target, local)
	if restored[0].Value != "first-secret" || restored[2].Value != "second-secret" {
		t.Fatalf("restored duplicate variables = %+v", restored)
	}
}

func TestRedactorScrubsSecretAcrossRequestFields(t *testing.T) {
	redactor := NewRedactor(nil, "super-secret")
	request := model.HttpRequest{
		Method:  "POST",
		Url:     "https://example.test/super-secret",
		Params:  []model.KV{{Key: "query", Value: "super-secret", Description: "uses super-secret"}},
		Headers: []model.KV{{Key: "X-Token", Value: "Bearer super-secret"}},
		Body: model.Body{
			Kind: "formdata", Text: `{"token":"super-secret"}`, Path: "C:/super-secret.bin",
			Query: "query { super-secret }", Variables: `{"token":"super-secret"}`,
			Items: []model.FormItem{{Key: "token", Type: "text", Value: "super-secret", Path: "super-secret.txt"}},
		},
		Auth:       model.Auth{Type: "bearer", Params: map[string]string{"token": "super-secret"}},
		PreScript:  "console.log('super-secret')",
		TestScript: "pm.expect('super-secret')",
	}
	redacted := redactor.Request(request)
	raw := fmt.Sprintf("%+v", redacted)
	if strings.Contains(raw, "super-secret") {
		t.Fatalf("request still contains secret: %s", raw)
	}
	if redacted.Auth.Params["token"] != redactedText {
		t.Fatalf("auth token = %q", redacted.Auth.Params["token"])
	}
	if request.Headers[0].Value != "Bearer super-secret" {
		t.Fatal("redaction mutated the source request")
	}
}
