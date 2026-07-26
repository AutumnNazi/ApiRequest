package storage

import (
	"database/sql"
	"encoding/json"

	"apirequest/backend/model"
)

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

// DeleteExample 软删除示例
func (s *Store) DeleteExample(exampleId string) error {
	_, err := s.db.Exec("UPDATE example SET deleted_at = ? WHERE id = ?", nowMs(), exampleId)
	return err
}
