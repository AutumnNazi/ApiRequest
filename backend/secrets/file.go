package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/argon2"
)

const (
	fileVersion           = 1
	maxEncryptedVaultSize = 16 << 20
)

type encryptedFile struct {
	Version    int    `json:"version"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

type fileBackend struct {
	path    string
	key     []byte
	salt    []byte
	entries map[string]string
}

func newFileBackend(dataDir string) *fileBackend {
	return &fileBackend{path: filepath.Join(dataDir, "secrets.vault")}
}

func (f *fileBackend) exists() bool {
	_, err := os.Stat(f.path)
	return err == nil
}

func (f *fileBackend) unlocked() bool { return len(f.key) > 0 && f.entries != nil }

func (f *fileBackend) unlock(password string) error {
	if f.unlocked() {
		return nil
	}
	data, err := readEncryptedVault(f.path)
	if errors.Is(err, os.ErrNotExist) {
		f.salt = make([]byte, 16)
		if _, err := rand.Read(f.salt); err != nil {
			return err
		}
		f.key = deriveKey(password, f.salt)
		f.entries = map[string]string{}
		if err := f.persist(); err != nil {
			f.lock()
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}
	var envelope encryptedFile
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("decode encrypted vault: %w", err)
	}
	if envelope.Version != fileVersion {
		return fmt.Errorf("unsupported encrypted vault version %d", envelope.Version)
	}
	salt, err := base64.StdEncoding.DecodeString(envelope.Salt)
	if err != nil {
		return fmt.Errorf("decode encrypted vault salt: %w", err)
	}
	nonce, err := base64.StdEncoding.DecodeString(envelope.Nonce)
	if err != nil {
		return fmt.Errorf("decode encrypted vault nonce: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(envelope.Ciphertext)
	if err != nil {
		return fmt.Errorf("decode encrypted vault ciphertext: %w", err)
	}
	key := deriveKey(password, salt)
	plaintext, err := decrypt(key, nonce, ciphertext)
	if err != nil {
		return errors.New("invalid master password or corrupt encrypted vault")
	}
	entries := map[string]string{}
	if err := json.Unmarshal(plaintext, &entries); err != nil {
		return fmt.Errorf("decode encrypted vault entries: %w", err)
	}
	f.key, f.salt, f.entries = key, salt, entries
	return nil
}

func (f *fileBackend) lock() {
	for i := range f.key {
		f.key[i] = 0
	}
	f.key = nil
	f.salt = nil
	f.entries = nil
}

func (f *fileBackend) put(id, value string) error {
	if !f.unlocked() {
		return ErrLocked
	}
	previous, existed := f.entries[id]
	f.entries[id] = value
	if err := f.persist(); err != nil {
		if existed {
			f.entries[id] = previous
		} else {
			delete(f.entries, id)
		}
		return err
	}
	return nil
}

func (f *fileBackend) get(id string) (string, error) {
	if !f.unlocked() {
		return "", ErrLocked
	}
	value, ok := f.entries[id]
	if !ok {
		return "", ErrNotFound
	}
	return value, nil
}

func (f *fileBackend) delete(id string) error {
	if !f.unlocked() {
		return ErrLocked
	}
	if _, ok := f.entries[id]; !ok {
		return nil
	}
	previous := f.entries[id]
	delete(f.entries, id)
	if err := f.persist(); err != nil {
		f.entries[id] = previous
		return err
	}
	return nil
}

func (f *fileBackend) persist() error {
	plaintext, err := json.Marshal(f.entries)
	if err != nil {
		return err
	}
	block, err := aes.NewCipher(f.key)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	envelope := encryptedFile{
		Version:    fileVersion,
		Salt:       base64.StdEncoding.EncodeToString(f.salt),
		Nonce:      base64.StdEncoding.EncodeToString(nonce),
		Ciphertext: base64.StdEncoding.EncodeToString(ciphertext),
	}
	data, err := json.MarshalIndent(envelope, "", "  ")
	if err != nil {
		return err
	}
	if len(data) > maxEncryptedVaultSize {
		return fmt.Errorf("encrypted vault exceeds %d byte limit", maxEncryptedVaultSize)
	}
	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(f.path), ".secrets-vault-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, f.path); err != nil {
		return err
	}
	committed = true
	return nil
}

func readEncryptedVault(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxEncryptedVaultSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxEncryptedVaultSize {
		return nil, fmt.Errorf("encrypted vault exceeds %d byte limit", maxEncryptedVaultSize)
	}
	return data, nil
}

func deriveKey(password string, salt []byte) []byte {
	return argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
}

func decrypt(key, nonce, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, errors.New("invalid nonce size")
	}
	return gcm.Open(nil, nonce, ciphertext, nil)
}
