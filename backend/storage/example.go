package storage

import (
	"database/sql"
	"encoding/json"
	"errors"

	"apirequest/backend/model"
	"apirequest/backend/secrets"
)

const exampleRedactionMigrationKey = "example.redaction.v2"

// ListExamples 列出请求节点下的示例
func (s *Store) ListExamples(nodeId string) ([]model.Example, error) {
	rows, err := s.db.Query(`
		SELECT id, node_id, name, request_snap, status, headers, body, created_at, updated_at
		FROM example WHERE node_id = ? AND deleted_at IS NULL ORDER BY created_at`, nodeId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanExamples(rows)
}

// ListExamplesForCollection 列出集合全部请求的示例（Mock Server 用）
func (s *Store) ListExamplesForCollection(collectionId string) ([]model.Example, error) {
	rows, err := s.db.Query(`
		WITH RECURSIVE sub(id) AS (
		  SELECT id FROM node WHERE id = ? AND deleted_at IS NULL
		  UNION ALL
		  SELECT n.id FROM node n JOIN sub ON n.parent_id = sub.id WHERE n.deleted_at IS NULL
		)
		SELECT e.id, e.node_id, e.name, e.request_snap, e.status, e.headers, e.body,
		       e.created_at, e.updated_at
		FROM example e JOIN sub ON e.node_id = sub.id
		WHERE e.deleted_at IS NULL ORDER BY e.created_at`, collectionId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanExamples(rows)
}

func scanExamples(rows *sql.Rows) ([]model.Example, error) {
	out := []model.Example{}
	for rows.Next() {
		var e model.Example
		var snap, body sql.NullString
		var headers string
		if err := rows.Scan(&e.Id, &e.NodeId, &e.Name, &snap, &e.Status, &headers, &body,
			&e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		e.Body = body.String
		if snap.Valid && snap.String != "" {
			var req model.HttpRequest
			if err := json.Unmarshal([]byte(snap.String), &req); err == nil {
				e.RequestSnap = &req
			}
		}
		if err := json.Unmarshal([]byte(headers), &e.Headers); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// UpsertExample 新增或更新示例
func (s *Store) UpsertExample(e model.Example) (model.Example, error) {
	now := nowMs()
	if e.Id == "" {
		e.Id = newId()
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	redactor := secrets.NewRedactor(s.vault)
	values := secrets.HeaderValues(e.Headers)
	if e.RequestSnap != nil {
		values = append(values, secrets.RequestCredentialValues(*e.RequestSnap)...)
		redactor = secrets.NewRedactor(s.vault, values...)
		redacted := redactor.Request(*e.RequestSnap)
		e.RequestSnap = &redacted
	} else {
		redactor = secrets.NewRedactor(s.vault, values...)
	}
	e.Headers = redactor.ResponseHeaders(e.Headers)
	e.Body = redactor.String(e.Body)
	if e.Headers == nil {
		e.Headers = []model.KV{}
	}
	headers, err := json.Marshal(e.Headers)
	if err != nil {
		return e, err
	}
	var snap sql.NullString
	if e.RequestSnap != nil {
		b, err := json.Marshal(e.RequestSnap)
		if err != nil {
			return e, err
		}
		snap = sql.NullString{String: string(b), Valid: true}
	}
	_, err = s.db.Exec(`
		INSERT INTO example (id, node_id, name, request_snap, status, headers, body, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		  name = excluded.name,
		  request_snap = excluded.request_snap,
		  status = excluded.status,
		  headers = excluded.headers,
		  body = excluded.body,
		  updated_at = excluded.updated_at`,
		e.Id, e.NodeId, e.Name, snap, e.Status, string(headers),
		sql.NullString{String: e.Body, Valid: e.Body != ""}, e.CreatedAt, e.UpdatedAt)
	return e, err
}

// migrateExampleSecrets removes credentials from legacy Mock examples. The
// example payload remains useful for Mock responses, but request credentials
// and Set-Cookie values are never recoverable from this audit-like surface.
func (s *Store) migrateExampleSecrets() error {
	done, err := s.GetSetting(exampleRedactionMigrationKey)
	if err != nil || done == "1" {
		return err
	}
	rows, err := s.db.Query(`SELECT id, COALESCE(request_snap, ''), headers, COALESCE(body, '') FROM example`)
	if err != nil {
		return err
	}
	type update struct{ id, request, headers, body string }
	updates := []update{}
	pendingUnlock := false
	for rows.Next() {
		var item update
		if err := rows.Scan(&item.id, &item.request, &item.headers, &item.body); err != nil {
			rows.Close()
			return err
		}
		redactor := secrets.NewRedactor(s.vault)
		var headers []model.KV
		if json.Unmarshal([]byte(item.headers), &headers) == nil {
			redactor = secrets.NewRedactor(s.vault, secrets.HeaderValues(headers)...)
		}
		if item.request != "" {
			var request model.HttpRequest
			if json.Unmarshal([]byte(item.request), &request) == nil {
				values, err := secrets.StoredRequestCredentialValues(s.vault, request)
				if errors.Is(err, secrets.ErrLocked) {
					pendingUnlock = true
					continue
				}
				if err != nil {
					rows.Close()
					return err
				}
				values = append(values, secrets.HeaderValues(headers)...)
				redactor = secrets.NewRedactor(s.vault, values...)
				request = redactor.Request(request)
				data, err := json.Marshal(request)
				if err != nil {
					rows.Close()
					return err
				}
				item.request = string(data)
			}
		}
		item.body = redactor.String(item.body)
		if json.Unmarshal([]byte(item.headers), &headers) == nil {
			data, err := json.Marshal(redactor.ResponseHeaders(headers))
			if err != nil {
				rows.Close()
				return err
			}
			item.headers = string(data)
		}
		updates = append(updates, item)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, item := range updates {
		if _, err := tx.Exec(`UPDATE example SET request_snap = NULLIF(?, ''), headers = ?, body = NULLIF(?, '') WHERE id = ?`, item.request, item.headers, item.body, item.id); err != nil {
			return err
		}
	}
	if !pendingUnlock {
		if _, err := tx.Exec(`
			INSERT INTO setting(key, value) VALUES (?, '1')
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, exampleRedactionMigrationKey); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteExample 软删除示例
func (s *Store) DeleteExample(exampleId string) error {
	_, err := s.db.Exec("UPDATE example SET deleted_at = ? WHERE id = ?", nowMs(), exampleId)
	return err
}
