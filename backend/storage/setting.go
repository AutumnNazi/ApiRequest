package storage

import (
	"database/sql"
	"errors"
	"sort"
)

const upsertSettingSQL = `
	INSERT INTO setting (key, value) VALUES (?,?)
	ON CONFLICT(key) DO UPDATE SET value = excluded.value`

type settingQueryer interface {
	QueryRow(query string, args ...any) *sql.Row
}

type settingExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func normalizeSettingKeys(keys []string) ([]string, error) {
	unique := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		if key == "" {
			return nil, errors.New("setting key is required")
		}
		unique[key] = struct{}{}
	}
	keys = keys[:0]
	for key := range unique {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}

func getSettings(queryer settingQueryer, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		var value string
		err := queryer.QueryRow("SELECT value FROM setting WHERE key = ?", key).Scan(&value)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		values[key] = value
	}
	return values, nil
}

func setSettings(execer settingExecer, values map[string]string) error {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	keys, err := normalizeSettingKeys(keys)
	if err != nil {
		return err
	}
	for _, key := range keys {
		if _, err := execer.Exec(upsertSettingSQL, key, values[key]); err != nil {
			return err
		}
	}
	return nil
}

// GetSetting 读设置项；不存在返回空串
func (s *Store) GetSetting(key string) (string, error) {
	values, err := getSettings(s.db, []string{key})
	if err != nil {
		return "", err
	}
	return values[key], nil
}

// SetSetting 写设置项
func (s *Store) SetSetting(key, value string) error {
	return setSettings(s.db, map[string]string{key: value})
}

// GetSettings reads a consistent snapshot of several setting values.
func (s *Store) GetSettings(keys []string) (map[string]string, error) {
	keys, err := normalizeSettingKeys(append([]string(nil), keys...))
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return map[string]string{}, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	values, err := getSettings(tx, keys)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return values, nil
}

// SetSettings atomically updates a group of related setting values.
func (s *Store) SetSettings(values map[string]string) error {
	if len(values) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := setSettings(tx, values); err != nil {
		return err
	}
	return tx.Commit()
}
