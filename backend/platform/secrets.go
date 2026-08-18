package platform

import (
	"errors"

	keyring "github.com/zalando/go-keyring"
)

// SecretStore is the platform credential-store surface consumed by the
// Vault. Implementations must never persist secret values in application
// settings or logs.
type SecretStore interface {
	Set(service, account, value string) error
	Get(service, account string) (string, error)
	Delete(service, account string) error
}

type systemSecretStore struct{}

func (systemSecretStore) Set(service, account, value string) error {
	return keyring.Set(service, account, value)
}

func (systemSecretStore) Get(service, account string) (string, error) {
	return keyring.Get(service, account)
}

func (systemSecretStore) Delete(service, account string) error {
	return keyring.Delete(service, account)
}

// SystemSecretStore returns the operating-system credential adapter backed by
// Windows Credential Manager, macOS Keychain, or the available Linux secret
// service.
func SystemSecretStore() SecretStore { return systemSecretStore{} }

// IsSecretNotFound normalizes the platform adapter's missing-entry result.
func IsSecretNotFound(err error) bool { return errors.Is(err, keyring.ErrNotFound) }
