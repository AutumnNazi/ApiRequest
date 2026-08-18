package platform

import (
	"errors"
	"fmt"
	"testing"

	keyring "github.com/zalando/go-keyring"
)

func TestSecretStoreContractAndMissingError(t *testing.T) {
	var _ SecretStore = SystemSecretStore()
	if !IsSecretNotFound(keyring.ErrNotFound) {
		t.Fatal("native missing-secret error was not recognized")
	}
	if !IsSecretNotFound(fmt.Errorf("wrapped: %w", keyring.ErrNotFound)) {
		t.Fatal("wrapped missing-secret error was not recognized")
	}
	if IsSecretNotFound(errors.New("keyring unavailable")) {
		t.Fatal("keyring outage was mistaken for a missing secret")
	}
}
