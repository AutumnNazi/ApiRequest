package storage

import (
	"database/sql"
	"strings"
	"time"

	"apirequest/backend/model"
)

// ListCookies 列出全部未过期 cookie（domain 空 = 全部）
func (s *Store) ListCookies(domain string) ([]model.Cookie, error) {
	now := time.Now().UnixMilli()
	q := `SELECT name, value, domain, path, expires_at, http_only, secure
	      FROM cookie WHERE (expires_at IS NULL OR expires_at = 0 OR expires_at > ?)`
	args := []any{now}
	if domain != "" {
		q += " AND domain LIKE ?"
		args = append(args, "%"+domain)
	}
	q += " ORDER BY domain, path, name"
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.Cookie{}
	for rows.Next() {
		var c model.Cookie
		var exp sql.NullInt64
		var httpOnly, secure int
		if err := rows.Scan(&c.Name, &c.Value, &c.Domain, &c.Path, &exp, &httpOnly, &secure); err != nil {
			return nil, err
		}
		c.Expires = exp.Int64
		c.HttpOnly = httpOnly != 0
		c.Secure = secure != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

// UpsertCookie 写入/更新 cookie；value 为空视为删除（HTTP 语义）
func (s *Store) UpsertCookie(c model.Cookie) error {
	if c.Path == "" {
		c.Path = "/"
	}
	if c.Value == "" || (c.Expires > 0 && c.Expires < time.Now().UnixMilli()) {
		_, err := s.db.Exec("DELETE FROM cookie WHERE domain = ? AND path = ? AND name = ?",
			c.Domain, c.Path, c.Name)
		return err
	}
	boolInt := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}
	var exp sql.NullInt64
	if c.Expires > 0 {
		exp = sql.NullInt64{Int64: c.Expires, Valid: true}
	}
	_, err := s.db.Exec(`
		INSERT INTO cookie (id, domain, path, name, value, expires_at, http_only, secure)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(domain, path, name) DO UPDATE SET
		  value = excluded.value,
		  expires_at = excluded.expires_at,
		  http_only = excluded.http_only,
		  secure = excluded.secure`,
		newId(), c.Domain, c.Path, c.Name, c.Value, exp, boolInt(c.HttpOnly), boolInt(c.Secure))
	return err
}

// DeleteCookie 删除单个 cookie
func (s *Store) DeleteCookie(domain, path, name string) error {
	if path == "" {
		path = "/"
	}
	_, err := s.db.Exec("DELETE FROM cookie WHERE domain = ? AND path = ? AND name = ?", domain, path, name)
	return err
}

// ClearCookies 清空（domain 空 = 全部）
func (s *Store) ClearCookies(domain string) error {
	if domain == "" {
		_, err := s.db.Exec("DELETE FROM cookie")
		return err
	}
	_, err := s.db.Exec("DELETE FROM cookie WHERE domain LIKE ?", "%"+domain)
	return err
}

// CookiesForHost 返回适用于 host 的 cookie（域匹配：完全一致或后缀域）
func (s *Store) CookiesForHost(host string) ([]model.Cookie, error) {
	all, err := s.ListCookies("")
	if err != nil {
		return nil, err
	}
	host = strings.ToLower(host)
	out := []model.Cookie{}
	for _, c := range all {
		d := strings.ToLower(strings.TrimPrefix(c.Domain, "."))
		if host == d || strings.HasSuffix(host, "."+d) {
			out = append(out, c)
		}
	}
	return out, nil
}
