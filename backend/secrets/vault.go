// Package secrets owns credential persistence and redaction policy.
package secrets

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"apirequest/backend/platform"
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

// Keyring is retained as the Vault-facing name for the platform credential
// adapter and as a stable test injection point.
type Keyring = platform.SecretStore

// SecretWriter is the write surface used by protection helpers. Vault and
// WriteBatch both implement it so callers can make DB + secret updates
// recoverable without exposing backend details.
type SecretWriter interface {
	Put(logicalKey, value string) (string, error)
	PutPlaintext(logicalKey, value string) (string, error)
	Delete(ref string) error
}

// IsKeyringRef reports whether value belongs to the system-keyring Adapter.
func IsKeyringRef(value string) bool {
	backend, _, err := parseRef(value)
	return err == nil && backend == "keyring"
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
	return NewWithKeyring(dataDir, platform.SystemSecretStore())
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
	return err == nil || platform.IsSecretNotFound(err) || errors.Is(err, ErrNotFound)
}

func (v *Vault) refreshKeyringAvailabilityLocked() {
	if !v.keyringAvailable {
		v.keyringAvailable = v.probeKeyring()
	}
}

// Status reports which persistence Adapter can currently serve secrets.
func (v *Vault) Status() Status {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.refreshKeyringAvailabilityLocked()
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
	v.refreshKeyringAvailabilityLocked()

	if v.keyringAvailable {
		previous, previousErr := v.keyring.Get(serviceName, id)
		existed := previousErr == nil
		if previousErr != nil && !isSecretNotFound(previousErr) {
			v.keyringAvailable = false
		} else if err := v.keyring.Set(serviceName, id, value); err == nil {
			v.remember(value)
			ref := keyringRef + id
			return ref, func() error {
				v.mu.Lock()
				defer v.mu.Unlock()
				if existed {
					if err := v.keyring.Set(serviceName, id, previous); err != nil {
						v.keyringAvailable = false
						return err
					}
					return nil
				}
				err := v.keyring.Delete(serviceName, id)
				if isSecretNotFound(err) {
					return nil
				}
				if err != nil {
					v.keyringAvailable = false
				}
				return err
			}, nil
		} else {
			// A keychain can disappear after startup (session lock/service failure).
			v.keyringAvailable = false
		}
	}
	if !v.file.unlocked() {
		return "", nil, fmt.Errorf("%w: no secret Adapter is currently writable", ErrLocked)
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
		v.refreshKeyringAvailabilityLocked()
		if !v.keyringAvailable {
			return nil, fmt.Errorf("%w: system keychain unavailable", ErrLocked)
		}
		previous, err := v.keyring.Get(serviceName, id)
		if isSecretNotFound(err) {
			return nil, nil
		}
		if err != nil {
			v.keyringAvailable = false
			return nil, fmt.Errorf("%w: system keychain unavailable: %v", ErrLocked, err)
		}
		if err := v.keyring.Delete(serviceName, id); err != nil {
			if isSecretNotFound(err) {
				return nil, nil
			}
			v.keyringAvailable = false
			return nil, fmt.Errorf("%w: system keychain unavailable: %v", ErrLocked, err)
		}
		return func() error {
			v.mu.Lock()
			defer v.mu.Unlock()
			if err := v.keyring.Set(serviceName, id, previous); err != nil {
				v.keyringAvailable = false
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
		v.refreshKeyringAvailabilityLocked()
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
		if isSecretNotFound(err) {
			return "", ErrNotFound
		}
		if backend == "keyring" {
			v.keyringAvailable = false
			return "", fmt.Errorf("%w: system keychain unavailable: %v", ErrLocked, err)
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
		v.refreshKeyringAvailabilityLocked()
		if !v.keyringAvailable {
			return fmt.Errorf("%w: system keychain unavailable", ErrLocked)
		}
		err = v.keyring.Delete(serviceName, id)
		if isSecretNotFound(err) {
			return nil
		}
		if err != nil {
			v.keyringAvailable = false
			return fmt.Errorf("%w: system keychain unavailable: %v", ErrLocked, err)
		}
		return err
	case "file":
		return v.file.delete(id)
	default:
		return ErrInvalidRef
	}
}

func isSecretNotFound(err error) bool {
	return platform.IsSecretNotFound(err) || errors.Is(err, ErrNotFound)
}

// RedactString removes every credential value observed by this Vault from text.
func (v *Vault) RedactString(input string) string {
	return redactKnown(input, v.knownValuesSnapshot())
}

func (v *Vault) knownValuesSnapshot() map[string]struct{} {
	v.mu.RLock()
	defer v.mu.RUnlock()
	values := make(map[string]struct{}, len(v.knownValues))
	for value := range v.knownValues {
		values[value] = struct{}{}
	}
	return values
}

func (v *Vault) remember(value string) {
	if value != "" && value != redactedText {
		v.knownValues[value] = struct{}{}
	}
}

func redactKnown(input string, values map[string]struct{}) string {
	ordered := make([]string, 0, len(values))
	for value := range values {
		if value != "" && value != redactedText {
			ordered = append(ordered, value)
		}
	}
	sort.Slice(ordered, func(i, j int) bool {
		if len(ordered[i]) == len(ordered[j]) {
			return ordered[i] < ordered[j]
		}
		return len(ordered[i]) > len(ordered[j])
	})
	for _, value := range ordered {
		input = strings.ReplaceAll(input, value, redactedText)
	}
	return input
}

func secretID(logicalKey string) string {
	sum := sha256.Sum256([]byte(logicalKey))
	return base64.RawURLEncoding.EncodeToString(sum[:24])
}

// IsRef reports whether value is an opaque Vault reference.
func IsRef(value string) bool {
	_, _, err := parseRef(value)
	return err == nil
}

// IsFileRef reports whether value belongs to the encrypted-file Adapter.
func IsFileRef(value string) bool {
	backend, _, err := parseRef(value)
	return err == nil && backend == "file"
}

// ReferenceMatchesLogicalKey reports whether ref carries the deterministic
// identifier assigned to logicalKey. It does not access the secret backend.
func ReferenceMatchesLogicalKey(ref, logicalKey string) bool {
	_, id, err := parseRef(ref)
	return err == nil && id == secretID(logicalKey)
}

// ReferencesShareIdentifier reports whether two canonical references point to
// the same deterministic logical secret through different persistence Adapters.
func ReferencesShareIdentifier(first, second string) bool {
	firstBackend, firstID, firstErr := parseRef(first)
	secondBackend, secondID, secondErr := parseRef(second)
	return firstErr == nil && secondErr == nil && firstBackend != secondBackend && firstID == secondID
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
	decoded, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil || len(decoded) != 24 {
		return "", "", ErrInvalidRef
	}
	return backend, id, nil
}
