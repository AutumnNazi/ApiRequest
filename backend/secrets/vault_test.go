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
	failSet     bool
}

func (f *fakeKeyring) Set(_, account, value string) error {
	if f.unavailable || f.failSet {
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

func TestStatusRediscoversRecoveredKeyring(t *testing.T) {
	adapter := &fakeKeyring{}
	vault := NewWithKeyring(t.TempDir(), adapter)
	ref, err := vault.Put("node/n1/auth/token", "recoverable-secret")
	if err != nil {
		t.Fatal(err)
	}

	adapter.failSet = true
	if _, err := vault.Put("node/n2/auth/token", "transient-write"); !errors.Is(err, ErrLocked) {
		t.Fatalf("transient keyring failure error = %v", err)
	}
	adapter.failSet = false

	if status := vault.Status(); status.Mode != "keyring" || !status.KeyringAvailable || !status.CanStore {
		t.Fatalf("recovered keyring status = %+v", status)
	}
	if value, err := vault.Resolve(ref); err != nil || value != "recoverable-secret" {
		t.Fatalf("recovered keyring value = %q, err = %v", value, err)
	}
	recoveredRef, err := vault.Put("node/n2/auth/token", "recovered-write")
	if err != nil || !IsKeyringRef(recoveredRef) {
		t.Fatalf("recovered keyring write = %q, err = %v", recoveredRef, err)
	}
}

func TestKeyringReadFailureFallsBackToUnlockedFile(t *testing.T) {
	adapter := &fakeKeyring{}
	vault := NewWithKeyring(t.TempDir(), adapter)
	if err := vault.Unlock("fallback-after-keyring-read-failure"); err != nil {
		t.Fatal(err)
	}
	adapter.unavailable = true

	ref, err := vault.Put("node/n1/auth/token", "file-fallback-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !IsFileRef(ref) {
		t.Fatalf("fallback ref = %q", ref)
	}
	if value, err := vault.Resolve(ref); err != nil || value != "file-fallback-secret" {
		t.Fatalf("fallback value = %q, err = %v", value, err)
	}
}

func TestKeyringResolveFailureUpdatesStatusAndRecovers(t *testing.T) {
	adapter := &fakeKeyring{}
	vault := NewWithKeyring(t.TempDir(), adapter)
	ref, err := vault.Put("node/n1/auth/token", "recover-after-read")
	if err != nil {
		t.Fatal(err)
	}
	adapter.unavailable = true
	if _, err := vault.Resolve(ref); !errors.Is(err, ErrLocked) {
		t.Fatalf("keyring outage resolve error = %v", err)
	}
	if status := vault.Status(); status.KeyringAvailable || status.CanStore {
		t.Fatalf("keyring outage status = %+v", status)
	}

	adapter.unavailable = false
	if status := vault.Status(); !status.KeyringAvailable || !status.CanStore {
		t.Fatalf("recovered keyring status = %+v", status)
	}
	if value, err := vault.Resolve(ref); err != nil || value != "recover-after-read" {
		t.Fatalf("recovered keyring value = %q, err = %v", value, err)
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

	request := model.HttpRequest{
		Auth: auth,
		Params: []model.KV{
			{Key: "api_token", Value: "query-secret", Enabled: true},
			{Key: "page", Value: "1", Enabled: true},
		},
		Headers: []model.KV{
			{Key: "Authorization", Value: "Bearer header-secret", Enabled: true},
			{Key: "X-Custom-Token", Value: "disabled-secret", Enabled: false},
			{Key: "X-Trace", Value: "public-trace", Enabled: true},
		},
		Body: model.Body{Kind: "formdata", Items: []model.FormItem{
			{Key: "password", Type: "text", Value: "form-secret", Enabled: true},
			{Key: "password", Type: "file", Path: "C:/fixtures/password.txt", Enabled: true},
		}},
	}
	protectedRequest, err := ProtectRequest(vault, request, "node/n1/request")
	if err != nil {
		t.Fatal(err)
	}
	if !IsRef(protectedRequest.Headers[0].Value) || !IsRef(protectedRequest.Headers[1].Value) {
		t.Fatalf("sensitive headers were not protected: %+v", protectedRequest.Headers)
	}
	if protectedRequest.Headers[2].Value != "public-trace" {
		t.Fatalf("public header was protected: %+v", protectedRequest.Headers)
	}
	if !IsRef(protectedRequest.Params[0].Value) || protectedRequest.Params[1].Value != "1" ||
		!IsRef(protectedRequest.Body.Items[0].Value) || protectedRequest.Body.Items[1].Path != "C:/fixtures/password.txt" {
		t.Fatalf("structured request values were not protected correctly: %+v", protectedRequest)
	}
	if refs := RequestReferences(protectedRequest); len(refs) != 6 {
		t.Fatalf("request refs = %+v, want auth and header references", refs)
	}
	resolvedRequest, err := ResolveRequest(vault, protectedRequest)
	if err != nil {
		t.Fatal(err)
	}
	if resolvedRequest.Headers[0].Value != "Bearer header-secret" || resolvedRequest.Headers[1].Value != "disabled-secret" {
		t.Fatalf("resolved headers = %+v", resolvedRequest.Headers)
	}
	if resolvedRequest.Params[0].Value != "query-secret" || resolvedRequest.Body.Items[0].Value != "form-secret" {
		t.Fatalf("resolved structured values = %+v", resolvedRequest)
	}
	redacted := RedactRequest(request)
	if redacted.Auth.Params["clientSecret"] != redactedText || redacted.Auth.Params["access_token"] != redactedText {
		t.Fatalf("redacted request = %+v", redacted)
	}
	if redacted.Auth.Params["clientId"] != "public-client" {
		t.Fatalf("public auth parameter was redacted: %+v", redacted)
	}
	if redacted.Headers[0].Value != redactedText || redacted.Headers[1].Value != redactedText || redacted.Headers[2].Value != "public-trace" {
		t.Fatalf("header redaction = %+v", redacted.Headers)
	}
	if redacted.Params[0].Value != redactedText || redacted.Params[1].Value != "1" ||
		redacted.Body.Items[0].Value != redactedText || redacted.Body.Items[1].Path != "C:/fixtures/password.txt" {
		t.Fatalf("structured value redaction = %+v", redacted)
	}
}

func TestRequestHeaderSecretsKeepDuplicateOccurrencesIndependent(t *testing.T) {
	vault := NewWithKeyring(t.TempDir(), &fakeKeyring{})
	request := model.HttpRequest{Headers: []model.KV{
		{Key: "X-Api-Token", Value: "first-secret", Enabled: true},
		{Key: "x-api-token", Value: "second-secret", Enabled: false},
	}}
	protected, err := ProtectRequest(vault, request, "node/n1/request")
	if err != nil {
		t.Fatal(err)
	}
	if protected.Headers[0].Value == protected.Headers[1].Value {
		t.Fatalf("duplicate headers share reference %q", protected.Headers[0].Value)
	}

	omitted := OmitNodeSecrets(model.Node{Request: &request})
	if omitted.Request.Headers[0].Value != "" || omitted.Request.Headers[1].Value != "" {
		t.Fatalf("omitted headers = %+v", omitted.Request.Headers)
	}
	RestoreOmittedNodeSecrets(&omitted, model.Node{Request: &request})
	if omitted.Request.Headers[0].Value != "first-secret" || omitted.Request.Headers[1].Value != "second-secret" {
		t.Fatalf("restored duplicate headers = %+v", omitted.Request.Headers)
	}
}

func TestRequestStructuredSecretsRestoreDuplicateOccurrences(t *testing.T) {
	request := model.HttpRequest{
		Params: []model.KV{
			{Key: "token", Value: "first-query", Enabled: true},
			{Key: "TOKEN", Value: "second-query", Enabled: false},
		},
		Body: model.Body{Items: []model.FormItem{
			{Key: "secret", Type: "text", Value: "first-form", Enabled: true},
			{Key: "SECRET", Type: "text", Value: "second-form", Enabled: false},
		}},
	}
	omitted := OmitNodeSecrets(model.Node{Request: &request})
	RestoreOmittedNodeSecrets(&omitted, model.Node{Request: &request})
	if omitted.Request.Params[0].Value != "first-query" || omitted.Request.Params[1].Value != "second-query" ||
		omitted.Request.Body.Items[0].Value != "first-form" || omitted.Request.Body.Items[1].Value != "second-form" {
		t.Fatalf("restored structured values = %+v", omitted.Request)
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

func TestVaultReferenceRequiresCanonicalIdentifier(t *testing.T) {
	for _, value := range []string{
		"secret://file/literal-user-value",
		"secret://keyring/not/base64/path",
		"secret://file/",
	} {
		if IsRef(value) {
			t.Fatalf("non-canonical value was accepted as Vault reference: %q", value)
		}
	}
	vault := NewWithKeyring(t.TempDir(), &fakeKeyring{})
	ref, err := vault.Put("test/canonical-reference", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if !IsRef(ref) {
		t.Fatalf("generated reference was rejected: %q", ref)
	}
	if !ReferenceMatchesLogicalKey(ref, "test/canonical-reference") {
		t.Fatalf("generated reference does not match its logical key: %q", ref)
	}
	if ReferenceMatchesLogicalKey(ref, "test/different-reference") {
		t.Fatalf("reference matched a different logical key: %q", ref)
	}
}

func TestReferencesShareIdentifierOnlyAcrossAdapters(t *testing.T) {
	vault := NewWithKeyring(t.TempDir(), &fakeKeyring{})
	keyringReference, err := vault.Put("test/shared-reference", "secret")
	if err != nil {
		t.Fatal(err)
	}
	fileEquivalent := fileRef + strings.TrimPrefix(keyringReference, keyringRef)
	if !ReferencesShareIdentifier(keyringReference, fileEquivalent) {
		t.Fatalf("references should share identifier: %q %q", keyringReference, fileEquivalent)
	}
	if ReferencesShareIdentifier(keyringReference, keyringReference) {
		t.Fatal("same Adapter reference reported as cross-Adapter replacement")
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

func TestRedactorHandlesOverlappingAndShortCredentialsDeterministically(t *testing.T) {
	redactor := NewRedactor(nil, "abc", "abcdef", "x")
	const input = "long=abcdef short=abc tiny=x"
	for i := 0; i < 100; i++ {
		if got := redactor.String(input); got != "long=<redacted> short=<redacted> tiny=<redacted>" {
			t.Fatalf("redaction pass %d = %q", i, got)
		}
	}
}

func TestRedactorHandlesOverlappingCredentialsAcrossVaultAndRequest(t *testing.T) {
	vault := NewWithKeyring(t.TempDir(), &fakeKeyring{})
	if _, err := vault.Put("test/overlapping-vault-value", "abcdef"); err != nil {
		t.Fatal(err)
	}
	redactor := NewRedactor(vault, "abc")
	if got := redactor.String("long=abcdef short=abc"); got != "long=<redacted> short=<redacted>" {
		t.Fatalf("cross-source redaction = %q", got)
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
