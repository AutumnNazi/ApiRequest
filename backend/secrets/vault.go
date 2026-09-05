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
	// knownOrder 记录 knownValues 的插入顺序，供超限时 FIFO 淘汰最旧一条
	knownOrder []string
}

// maxKnownValues 已知明文凭据的内存缓存上限（脱敏用）；超限时逐条淘汰最旧的
const maxKnownValues = 4096

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
	v.mu.RLock()
	keyringAvailable := v.keyringAvailable
	fileExists := v.file.exists()
	fileUnlocked := v.file.unlocked()
	v.mu.RUnlock()
	if !keyringAvailable {
		// 探测是系统 IO（可达数百毫秒），必须在锁外做。
		// 只读不回写：Status 是查询，探测窗口期间 Resolve 等写入方可能刚把
		// keyringAvailable 置为 false（真实失败），回写这里的乐观结论会覆盖掉它。
		// 恢复由下一次 Resolve 的双检路径落实，不依赖 Status 的副作用
		keyringAvailable = v.probeKeyring()
	}
	status := Status{
		KeyringAvailable: keyringAvailable,
		FileExists:       fileExists,
		FileUnlocked:     fileUnlocked,
	}
	switch {
	case keyringAvailable:
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
	// knownOrder 必须与 knownValues 一同清空：只清 map 会让明文残留在切片里
	// （违背 Lock 的"清除内存中解密内容"语义），且两者失同步后 FIFO 淘汰会去
	// delete 一个已不存在的 key，使 knownValues 突破 maxKnownValues 无界增长
	v.knownValues = map[string]struct{}{}
	v.knownOrder = nil
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
	var resolved string
	switch backend {
	case "keyring":
		// keyring 调用（系统凭据管理器，可达数百毫秒）移出锁外：
		// 持写锁做 IO 会阻塞所有请求的凭据解析
		v.mu.RLock()
		available := v.keyringAvailable
		keyring := v.keyring
		v.mu.RUnlock()
		if !available {
			// 双检：可能刚被探测为可用（如 keyring 后启用）
			v.mu.Lock()
			v.refreshKeyringAvailabilityLocked()
			available = v.keyringAvailable
			v.mu.Unlock()
			if !available {
				return "", fmt.Errorf("%w: system keychain unavailable", ErrLocked)
			}
		}
		resolved, err = keyring.Get(serviceName, id)
		if err != nil {
			if isSecretNotFound(err) {
				return "", ErrNotFound
			}
			v.mu.Lock()
			v.keyringAvailable = false
			v.mu.Unlock()
			return "", fmt.Errorf("%w: system keychain unavailable: %v", ErrLocked, err)
		}
	case "file":
		// get 是纯内存 map 读取（无 IO），须在锁内完成：
		// entries 由 put/delete 在写锁下改写，锁外读构成数据竞争
		v.mu.RLock()
		resolved, err = v.file.get(id)
		v.mu.RUnlock()
		if err != nil {
			if isSecretNotFound(err) {
				return "", ErrNotFound
			}
			return "", err
		}
	default:
		return "", ErrInvalidRef
	}
	v.mu.Lock()
	v.remember(resolved)
	v.mu.Unlock()
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
	if value == "" || value == redactedText {
		return
	}
	if _, seen := v.knownValues[value]; seen {
		return
	}
	// 有界缓存：避免明文凭据无界驻留内存。满了只淘汰最早记录的一条，
	// 不能整体清空——那会让此前所有凭据的脱敏同时失效，明文可能落日志。
	// 被淘汰的值若仍在使用，下次 Resolve 会重新记录（自愈）
	if len(v.knownValues) >= maxKnownValues {
		oldest := v.knownOrder[0]
		v.knownOrder = v.knownOrder[1:]
		delete(v.knownValues, oldest)
	}
	v.knownValues[value] = struct{}{}
	v.knownOrder = append(v.knownOrder, value)
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
