// Package secrets owns credential persistence and redaction policy.
package secrets

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	keyringlib "github.com/zalando/go-keyring"
)

const (
	serviceName  = "ApiRequest"
	keyringRef   = "secret://keyring/"
	fileRef      = "secret://file/"
	redactedText = "<redacted>"
)

var (
	// ErrLocked means an encrypted-file secret cannot be used until the vault is unlocked.
	ErrLocked = errors.New("secret vault is locked")
	// ErrInvalidRef means a secret reference has an unsupported or malformed format.
	ErrInvalidRef = errors.New("invalid secret reference")
	// ErrNotFound means a referenced secret no longer exists in its backing store.
	ErrNotFound = errors.New("secret not found")
)

// Keyring is the narrow Adapter required from an operating-system credential store.
type Keyring interface {
	Set(service, account, value string) error
	Get(service, account string) (string, error)
	Delete(service, account string) error
}

// SecretWriter is the write surface used by protection helpers. Vault and
// WriteBatch both implement it so callers can make DB + secret updates
// recoverable without exposing backend details.
type SecretWriter interface {
	Put(logicalKey, value string) (string, error)
	PutPlaintext(logicalKey, value string) (string, error)
	Delete(ref string) error
}

type systemKeyring struct{}

func (systemKeyring) Set(service, account, value string) error {
	return keyringlib.Set(service, account, value)
}
func (systemKeyring) Get(service, account string) (string, error) {
	return keyringlib.Get(service, account)
}
func (systemKeyring) Delete(service, account string) error {
	return keyringlib.Delete(service, account)
}

// Status is safe to expose to the UI; it never contains credential material.
type Status struct {
	Mode             string `json:"mode"` // keyring | file | locked
	KeyringAvailable bool   `json:"keyringAvailable"`
	FileExists       bool   `json:"fileExists"`
	FileUnlocked     bool   `json:"fileUnlocked"`
	CanStore         bool   `json:"canStore"`
}

// Vault selects the keychain when available and otherwise uses an unlocked encrypted file.
// Resolved values are cached only in memory so the Redactor can scrub logs consistently.
type Vault struct {
	mu               sync.RWMutex
	keyring          Keyring
	keyringAvailable bool
	file             *fileBackend
	knownValues      map[string]struct{}
}

// New constructs a production Vault rooted in dataDir.
func New(dataDir string) *Vault {
	return NewWithKeyring(dataDir, systemKeyring{})
}

// NewWithKeyring is an injection point for deterministic tests and alternate platform Adapters.
func NewWithKeyring(dataDir string, keyring Keyring) *Vault {
	v := &Vault{
		keyring:     keyring,
		file:        newFileBackend(dataDir),
		knownValues: map[string]struct{}{},
	}
	v.keyringAvailable = v.probeKeyring()
	return v
}

func (v *Vault) probeKeyring() bool {
	if v.keyring == nil {
		return false
	}
	_, err := v.keyring.Get(serviceName, "__availability_probe__")
	return err == nil || errors.Is(err, keyringlib.ErrNotFound) || errors.Is(err, ErrNotFound)
}

// Status reports which persistence Adapter can currently serve secrets.
func (v *Vault) Status() Status {
	v.mu.RLock()
	defer v.mu.RUnlock()
	status := Status{
		KeyringAvailable: v.keyringAvailable,
		FileExists:       v.file.exists(),
		FileUnlocked:     v.file.unlocked(),
	}
	switch {
	case v.keyringAvailable:
		status.Mode = "keyring"
		status.CanStore = true
	case status.FileUnlocked:
		status.Mode = "file"
		status.CanStore = true
	default:
		status.Mode = "locked"
	}
	return status
}

// Unlock decrypts or initializes the encrypted-file Adapter.
func (v *Vault) Unlock(password string) error {
	if strings.TrimSpace(password) == "" {
		return errors.New("master password is required")
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.file.unlock(password)
}

// Lock clears the fallback key and decrypted entries from memory.
func (v *Vault) Lock() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.file.lock()
	v.knownValues = map[string]struct{}{}
}

// Put persists value and returns a stable opaque reference derived from logicalKey.
func (v *Vault) Put(logicalKey, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if IsRef(value) {
		return value, nil
	}
	return v.putValue(logicalKey, value)
}

// WriteBatch records enough state to roll back Vault writes when the paired
// database operation fails. Writes remain durable immediately; Rollback puts
// the previous value back or removes a newly-created entry.
type WriteBatch struct {
	v      *Vault
	undos  []func() error
	closed bool
}

// BeginWrite starts a recoverable group of secret writes.
func (v *Vault) BeginWrite() *WriteBatch { return &WriteBatch{v: v} }

// Put persists a value and remembers how to undo this write.
func (b *WriteBatch) Put(logicalKey, value string) (string, error) {
	if b == nil || b.v == nil || b.closed {
		return "", errors.New("secret write batch is closed")
	}
	if value == "" || IsRef(value) {
		return b.v.Put(logicalKey, value)
	}
	ref, undo, err := b.v.putValueWithUndo(logicalKey, value)
	if err != nil {
		return "", err
	}
	if undo != nil {
		b.undos = append(b.undos, undo)
	}
	return ref, nil
}

// PutPlaintext is the batch equivalent of Vault.PutPlaintext.
func (b *WriteBatch) PutPlaintext(logicalKey, value string) (string, error) {
	if b == nil || b.v == nil || b.closed {
		return "", errors.New("secret write batch is closed")
	}
	if value == "" {
		return "", nil
	}
	ref, undo, err := b.v.putValueWithUndo(logicalKey, value)
	if err != nil {
		return "", err
	}
	if undo != nil {
		b.undos = append(b.undos, undo)
	}
	return ref, nil
}

// Delete removes a referenced value and records enough state to restore it if
// the paired database write fails.
func (b *WriteBatch) Delete(ref string) error {
	if b == nil || b.v == nil || b.closed {
		return errors.New("secret write batch is closed")
	}
	undo, err := b.v.deleteValueWithUndo(ref)
	if err != nil {
		return err
	}
	if undo != nil {
		b.undos = append(b.undos, undo)
	}
	return nil
}

// Commit closes the batch after its paired DB write succeeds.
func (b *WriteBatch) Commit() {
	if b != nil {
		b.closed = true
		b.undos = nil
	}
}

// Rollback restores all writes in reverse order. The first error is returned,
// but every undo is attempted so one failed backend call cannot strand the
// remaining changes.
func (b *WriteBatch) Rollback() error {
	if b == nil || b.closed {
		return nil
	}
	b.closed = true
	var firstErr error
	for i := len(b.undos) - 1; i >= 0; i-- {
		if err := b.undos[i](); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	b.undos = nil
	return firstErr
}

// PutPlaintext persists a caller-provided value even when it happens to look
// like an opaque Vault reference. UI boundaries must not interpret user text
// as an internal reference.
func (v *Vault) PutPlaintext(logicalKey, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	return v.putValue(logicalKey, value)
}

func (v *Vault) putValue(logicalKey, value string) (string, error) {
	ref, _, err := v.putValueWithUndo(logicalKey, value)
	return ref, err
}

func (v *Vault) putValueWithUndo(logicalKey, value string) (string, func() error, error) {
	id := secretID(logicalKey)
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.keyringAvailable {
		previous, previousErr := v.keyring.Get(serviceName, id)
		existed := previousErr == nil
		if previousErr != nil && !errors.Is(previousErr, keyringlib.ErrNotFound) && !errors.Is(previousErr, ErrNotFound) {
			return "", nil, previousErr
		}
		if err := v.keyring.Set(serviceName, id, value); err == nil {
			v.remember(value)
			ref := keyringRef + id
			return ref, func() error {
				v.mu.Lock()
				defer v.mu.Unlock()
				if existed {
					return v.keyring.Set(serviceName, id, previous)
				}
				err := v.keyring.Delete(serviceName, id)
				if errors.Is(err, keyringlib.ErrNotFound) || errors.Is(err, ErrNotFound) {
					return nil
				}
				return err
			}, nil
		}
		// A keychain can disappear after startup (session lock/service failure).
		v.keyringAvailable = false
	}
	if !v.file.unlocked() {
		return "", nil, ErrLocked
	}
	previous, existed := v.file.entries[id]
	if err := v.file.put(id, value); err != nil {
		return "", nil, err
	}
	v.remember(value)
	return fileRef + id, func() error {
		v.mu.Lock()
		defer v.mu.Unlock()
		if existed {
			return v.file.put(id, previous)
		}
		return v.file.delete(id)
	}, nil
}

func (v *Vault) deleteValueWithUndo(ref string) (func() error, error) {
	backend, id, err := parseRef(ref)
	if err != nil {
		return nil, err
	}
	v.mu.Lock()
	defer v.mu.Unlock()

	switch backend {
	case "keyring":
		previous, err := v.keyring.Get(serviceName, id)
		if isSecretNotFound(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if err := v.keyring.Delete(serviceName, id); err != nil {
			if isSecretNotFound(err) {
				return nil, nil
			}
			return nil, err
		}
		return func() error {
			v.mu.Lock()
			defer v.mu.Unlock()
			if err := v.keyring.Set(serviceName, id, previous); err != nil {
				return err
			}
			v.remember(previous)
			return nil
		}, nil
	case "file":
		previous, err := v.file.get(id)
		if errors.Is(err, ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		if err := v.file.delete(id); err != nil {
			return nil, err
		}
		return func() error {
			v.mu.Lock()
			defer v.mu.Unlock()
			if err := v.file.put(id, previous); err != nil {
				return err
			}
			v.remember(previous)
			return nil
		}, nil
	default:
		return nil, ErrInvalidRef
	}
}

// Resolve expands an opaque reference. Plain values pass through for legacy migration.
func (v *Vault) Resolve(value string) (string, error) {
	if value == "" || !IsRef(value) {
		return value, nil
	}
	backend, id, err := parseRef(value)
	if err != nil {
		return "", err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	var resolved string
	switch backend {
	case "keyring":
		if !v.keyringAvailable {
			return "", fmt.Errorf("%w: system keychain unavailable", ErrLocked)
		}
		resolved, err = v.keyring.Get(serviceName, id)
	case "file":
		resolved, err = v.file.get(id)
	default:
		err = ErrInvalidRef
	}
	if err != nil {
		if errors.Is(err, keyringlib.ErrNotFound) {
			return "", ErrNotFound
		}
		return "", err
	}
	v.remember(resolved)
	return resolved, nil
}

// Delete removes a referenced secret. Missing values are treated as already deleted.
func (v *Vault) Delete(ref string) error {
	backend, id, err := parseRef(ref)
	if err != nil {
		return err
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	switch backend {
	case "keyring":
		err = v.keyring.Delete(serviceName, id)
		if errors.Is(err, keyringlib.ErrNotFound) || errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	case "file":
		return v.file.delete(id)
	default:
		return ErrInvalidRef
	}
}

func isSecretNotFound(err error) bool {
	return errors.Is(err, keyringlib.ErrNotFound) || errors.Is(err, ErrNotFound)
}

// RedactString removes every credential value observed by this Vault from text.
func (v *Vault) RedactString(input string) string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return redactKnown(input, v.knownValues)
}

func (v *Vault) remember(value string) {
	if value != "" && value != redactedText {
		v.knownValues[value] = struct{}{}
	}
}

func redactKnown(input string, values map[string]struct{}) string {
	for value := range values {
		if len(value) >= 3 {
			input = strings.ReplaceAll(input, value, redactedText)
		}
	}
	return input
}

func secretID(logicalKey string) string {
	sum := sha256.Sum256([]byte(logicalKey))
	return base64.RawURLEncoding.EncodeToString(sum[:24])
}

// IsRef reports whether value is an opaque Vault reference.
func IsRef(value string) bool {
	return strings.HasPrefix(value, keyringRef) || strings.HasPrefix(value, fileRef)
}

func parseRef(ref string) (string, string, error) {
	var backend, id string
	switch {
	case strings.HasPrefix(ref, keyringRef):
		backend, id = "keyring", strings.TrimPrefix(ref, keyringRef)
	case strings.HasPrefix(ref, fileRef):
		backend, id = "file", strings.TrimPrefix(ref, fileRef)
	default:
		return "", "", ErrInvalidRef
	}
	if id == "" || strings.ContainsAny(id, `/\\`) || strings.Contains(id, "..") {
		return "", "", ErrInvalidRef
	}
	return backend, id, nil
}
