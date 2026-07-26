package storage

import "database/sql"

// GetSetting 读设置项；不存在返回空串
func (s *Store) GetSetting(key string) (string, error) {
	var v string
	err := s.db.QueryRow("SELECT value FROM setting WHERE key = ?", key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

// SetSetting 写设置项
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(`
		INSERT INTO setting (key, value) VALUES (?,?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}
