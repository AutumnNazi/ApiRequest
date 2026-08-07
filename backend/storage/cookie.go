package storage

import (
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"golang.org/x/net/publicsuffix"

	"apirequest/backend/model"
	"apirequest/backend/secrets"
)

const cookieSecretPrefix = "cookie/"

// ListCookies lists unexpired cookies and resolves their values at the Secret Vault seam.
func (s *Store) ListCookies(domain string) ([]model.Cookie, error) {
	query := `SELECT name, value, domain, path, expires_at, http_only, secure, same_site, host_only
	          FROM cookie WHERE (expires_at IS NULL OR expires_at = 0 OR expires_at > ?)`
	args := []any{time.Now().UnixMilli()}
	if domain != "" {
		query += " AND domain LIKE ?"
		args = append(args, "%"+domain)
	}
	query += " ORDER BY domain, length(path) DESC, path, name"
	return s.queryCookies(query, args...)
}

// CookiesForHost returns only cookies eligible for host. Filtering happens in SQLite so unrelated
// secret values are not fetched from the keychain on every request.
func (s *Store) CookiesForHost(host string) ([]model.Cookie, error) {
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if host == "" {
		return []model.Cookie{}, nil
	}
	query := `SELECT name, value, domain, path, expires_at, http_only, secure, same_site, host_only
	          FROM cookie
	          WHERE (expires_at IS NULL OR expires_at = 0 OR expires_at > ?)
	            AND ((host_only = 1 AND lower(ltrim(domain, '.')) = ?)
	              OR (host_only = 0 AND (? = lower(ltrim(domain, '.'))
	                OR ? LIKE '%.' || lower(ltrim(domain, '.')))))
	          ORDER BY length(path) DESC, path, name`
	return s.queryCookies(query, time.Now().UnixMilli(), host, host, host)
}

func (s *Store) queryCookies(query string, args ...any) ([]model.Cookie, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.Cookie{}
	for rows.Next() {
		var cookie model.Cookie
		var expires sql.NullInt64
		var sameSite sql.NullString
		var httpOnly, secure, hostOnly int
		var storedValue string
		if err := rows.Scan(
			&cookie.Name, &storedValue, &cookie.Domain, &cookie.Path, &expires,
			&httpOnly, &secure, &sameSite, &hostOnly,
		); err != nil {
			return nil, err
		}
		cookie.Value, err = s.vault.Resolve(storedValue)
		if err != nil {
			return nil, fmt.Errorf("resolve cookie %s for %s: %w", cookie.Name, cookie.Domain, err)
		}
		cookie.Expires = expires.Int64
		cookie.HttpOnly = httpOnly != 0
		cookie.Secure = secure != 0
		cookie.SameSite = sameSite.String
		cookie.HostOnly = hostOnly != 0
		out = append(out, cookie)
	}
	return out, rows.Err()
}

// UpsertCookie atomically coordinates one cookie metadata update with its Vault value.
func (s *Store) UpsertCookie(cookie model.Cookie) error {
	return s.UpsertCookies([]model.Cookie{cookie})
}

// UpsertCookies applies a response or import batch as one SQLite transaction and one recoverable
// Vault write batch. A failed item leaves neither partial cookie rows nor orphaned secrets.
func (s *Store) UpsertCookies(cookies []model.Cookie) error {
	if len(cookies) == 0 {
		return nil
	}
	return s.withSecretWrite(func(writer secrets.SecretWriter) error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		for _, cookie := range cookies {
			if err := upsertCookie(tx, writer, cookie); err != nil {
				return err
			}
		}
		return tx.Commit()
	})
}

func upsertCookie(tx *sql.Tx, writer secrets.SecretWriter, cookie model.Cookie) error {
	cookie.Domain = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(cookie.Domain)), ".")
	if cookie.Domain == "" || cookie.Name == "" {
		return errors.New("cookie domain and name are required")
	}
	if cookie.Path == "" || !strings.HasPrefix(cookie.Path, "/") {
		cookie.Path = "/"
	}
	cookie.SameSite = strings.ToLower(strings.TrimSpace(cookie.SameSite))
	if !cookie.HostOnly && isPublicSuffixCookieDomain(cookie.Domain) {
		return fmt.Errorf("cookie domain %q is a public suffix", cookie.Domain)
	}

	var id, oldValue string
	err := tx.QueryRow(
		"SELECT id, value FROM cookie WHERE domain = ? AND path = ? AND name = ?",
		cookie.Domain, cookie.Path, cookie.Name,
	).Scan(&id, &oldValue)
	exists := err == nil
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if cookie.MaxAge < 0 || (cookie.Expires > 0 && cookie.Expires <= time.Now().UnixMilli()) {
		if !exists {
			return nil
		}
		if secrets.IsRef(oldValue) {
			if err := writer.Delete(oldValue); err != nil {
				return err
			}
		}
		_, err = tx.Exec("DELETE FROM cookie WHERE id = ?", id)
		return err
	}
	if cookie.MaxAge > 0 {
		cookie.Expires = time.Now().Add(time.Duration(cookie.MaxAge) * time.Second).UnixMilli()
	}
	if !exists {
		id = newId()
	}
	storedValue, err := writer.PutPlaintext(cookieSecretPrefix+id+"/value", cookie.Value)
	if err != nil {
		return err
	}
	if secrets.IsRef(oldValue) && oldValue != storedValue {
		if err := writer.Delete(oldValue); err != nil {
			return err
		}
	}
	boolInt := func(value bool) int {
		if value {
			return 1
		}
		return 0
	}
	var expires sql.NullInt64
	if cookie.Expires > 0 {
		expires = sql.NullInt64{Int64: cookie.Expires, Valid: true}
	}
	_, err = tx.Exec(`
		INSERT INTO cookie (id, domain, path, name, value, expires_at, http_only, secure, same_site, host_only)
		VALUES (?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(domain, path, name) DO UPDATE SET
		  value = excluded.value,
		  expires_at = excluded.expires_at,
		  http_only = excluded.http_only,
		  secure = excluded.secure,
		  same_site = excluded.same_site,
		  host_only = excluded.host_only`,
		id, cookie.Domain, cookie.Path, cookie.Name, storedValue, expires,
		boolInt(cookie.HttpOnly), boolInt(cookie.Secure),
		sql.NullString{String: cookie.SameSite, Valid: cookie.SameSite != ""}, boolInt(cookie.HostOnly),
	)
	return err
}

func isPublicSuffixCookieDomain(domain string) bool {
	domain = strings.TrimPrefix(strings.ToLower(domain), ".")
	if domain == "" || domain == "localhost" || net.ParseIP(domain) != nil {
		return false
	}
	suffix, _ := publicsuffix.PublicSuffix(domain)
	return suffix == domain
}

// DeleteCookie removes both the metadata row and its referenced Vault value.
func (s *Store) DeleteCookie(domain, path, name string) error {
	if path == "" {
		path = "/"
	}
	return s.UpsertCookie(model.Cookie{
		Domain: domain, Path: path, Name: name, MaxAge: -1,
	})
}

// ClearCookies clears a domain filter or the complete global Jar and removes owned Vault values.
func (s *Store) ClearCookies(domain string) error {
	return s.withSecretWrite(func(writer secrets.SecretWriter) error {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		defer tx.Rollback()
		query := "SELECT value FROM cookie"
		args := []any{}
		if domain != "" {
			query += " WHERE domain LIKE ?"
			args = append(args, "%"+domain)
		}
		rows, err := tx.Query(query, args...)
		if err != nil {
			return err
		}
		refs := []string{}
		for rows.Next() {
			var value string
			if err := rows.Scan(&value); err != nil {
				rows.Close()
				return err
			}
			if secrets.IsRef(value) {
				refs = append(refs, value)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		rows.Close()
		for _, ref := range refs {
			if err := writer.Delete(ref); err != nil {
				return err
			}
		}
		deleteQuery := "DELETE FROM cookie"
		if domain != "" {
			deleteQuery += " WHERE domain LIKE ?"
		}
		if _, err := tx.Exec(deleteQuery, args...); err != nil {
			return err
		}
		return tx.Commit()
	})
}
