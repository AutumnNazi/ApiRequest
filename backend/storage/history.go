package storage

import (
	"database/sql"
	"encoding/json"

	"apirequest/backend/model"
)

// responseMeta history.response_meta 列的 JSON 结构
type responseMeta struct {
	Headers []model.KV   `json:"headers"`
	Timing  model.Timing `json:"timing"`
}

// InsertHistory 落一条历史；返回生成的 id
func (s *Store) InsertHistory(item model.HistoryItem) (string, error) {
	if item.Id == "" {
		item.Id = newId()
	}
	if item.CreatedAt == 0 {
		item.CreatedAt = nowMs()
	}
	snap, err := json.Marshal(item.RequestSnap)
	if err != nil {
		return "", err
	}
	meta, err := json.Marshal(responseMeta{Headers: item.RespHeaders, Timing: item.Timing})
	if err != nil {
		return "", err
	}
	var tests sql.NullString
	if len(item.TestResults) > 0 {
		b, err := json.Marshal(item.TestResults)
		if err != nil {
			return "", err
		}
		tests = sql.NullString{String: string(b), Valid: true}
	}
	_, err = s.db.Exec(`
		INSERT INTO history (id, workspace_id, request_snap, status, duration_ms,
		                     size_bytes, response_meta, body_ref, body_inline, test_results, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		item.Id, item.WorkspaceId, string(snap), item.Status, item.DurationMs,
		item.SizeBytes, string(meta),
		sql.NullString{String: item.BodyRef, Valid: item.BodyRef != ""},
		sql.NullString{String: item.BodyInline, Valid: item.BodyInline != ""},
		tests, item.CreatedAt)
	return item.Id, err
}

// ListHistory 按时间倒序返回历史（列表不含 body 内容以外的大字段）
func (s *Store) ListHistory(workspaceId string, q model.HistoryQuery) ([]model.HistoryItem, error) {
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	// request_snap 是 JSON 文本，模糊搜索直接 LIKE（低频路径，够用）
	args := []any{workspaceId}
	where := "workspace_id = ?"
	if q.Search != "" {
		where += " AND request_snap LIKE ?"
		args = append(args, "%"+q.Search+"%")
	}
	args = append(args, limit, q.Offset)

	rows, err := s.db.Query(`
		SELECT id, workspace_id, request_snap, status, duration_ms, size_bytes,
		       response_meta, body_ref, body_inline, test_results, created_at
		FROM history WHERE `+where+`
		ORDER BY created_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.HistoryItem{}
	for rows.Next() {
		var it model.HistoryItem
		var snap, meta string
		var bodyRef, bodyInline, tests sql.NullString
		if err := rows.Scan(&it.Id, &it.WorkspaceId, &snap, &it.Status, &it.DurationMs,
			&it.SizeBytes, &meta, &bodyRef, &bodyInline, &tests, &it.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(snap), &it.RequestSnap); err != nil {
			return nil, err
		}
		var m responseMeta
		if err := json.Unmarshal([]byte(meta), &m); err != nil {
			return nil, err
		}
		it.RespHeaders = m.Headers
		it.Timing = m.Timing
		it.BodyRef = bodyRef.String
		it.BodyInline = bodyInline.String
		if tests.Valid && tests.String != "" {
			if err := json.Unmarshal([]byte(tests.String), &it.TestResults); err != nil {
				return nil, err
			}
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// ClearHistory 清空工作区历史，并清理关联的大响应体 blob 文件，避免磁盘泄漏。
func (s *Store) ClearHistory(workspaceId string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// 1. 先收集将删除的 blob 引用（DB 行删除后无法回查）
	rows, err := tx.Query(
		"SELECT body_ref FROM history WHERE workspace_id = ? AND body_ref != ''", workspaceId)
	if err != nil {
		return err
	}
	var refs []string
	for rows.Next() {
		var ref sql.NullString
		if err := rows.Scan(&ref); err != nil {
			rows.Close()
			return err
		}
		if ref.Valid && ref.String != "" {
			refs = append(refs, ref.String)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	// 2. 删除 DB 行
	if _, err := tx.Exec("DELETE FROM history WHERE workspace_id = ?", workspaceId); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	// 3. 清理孤儿 blob 文件（单个失败不阻断，记录继续）
	for _, ref := range refs {
		s.removeBlobFile(ref)
	}
	return nil
}
