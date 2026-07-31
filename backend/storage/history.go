package storage

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"apirequest/backend/model"
	"apirequest/backend/secrets"
)

type responseMeta struct {
	Headers []model.KV   `json:"headers"`
	Timing  model.Timing `json:"timing"`
}

// InsertHistory writes one redacted detail record and returns its generated id.
func (s *Store) InsertHistory(item model.HistoryDetail) (string, error) {
	if item.Id == "" {
		item.Id = newId()
	}
	if item.CreatedAt == 0 {
		item.CreatedAt = nowMs()
	}
	historyRedactor := secrets.NewRedactor(s.vault, secrets.AuthValues(item.RequestSnap.Auth)...)
	item.RequestSnap = historyRedactor.Request(item.RequestSnap)
	snapshot, err := json.Marshal(item.RequestSnap)
	if err != nil {
		return "", err
	}
	meta, err := json.Marshal(responseMeta{Headers: item.RespHeaders, Timing: item.Timing})
	if err != nil {
		return "", err
	}
	var tests sql.NullString
	if len(item.TestResults) > 0 {
		data, err := json.Marshal(item.TestResults)
		if err != nil {
			return "", err
		}
		tests = sql.NullString{String: string(data), Valid: true}
	}
	_, err = s.db.Exec(`
		INSERT INTO history (id, workspace_id, request_snap, method, url, status, duration_ms,
		                     size_bytes, response_meta, body_ref, body_inline, test_results, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		item.Id, item.WorkspaceId, string(snapshot), item.RequestSnap.Method, item.RequestSnap.Url,
		item.Status, item.DurationMs, item.SizeBytes, string(meta),
		sql.NullString{String: item.BodyRef, Valid: item.BodyRef != ""},
		sql.NullString{String: item.BodyInline, Valid: item.BodyInline != ""},
		tests, item.CreatedAt)
	return item.Id, err
}

// ListHistory returns only bounded summary projections.
func (s *Store) ListHistory(workspaceId string, query model.HistoryQuery) (model.HistoryPage, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	where := []string{"workspace_id = ?"}
	args := []any{workspaceId}
	if query.Search != "" {
		where = append(where, "(method LIKE ? OR url LIKE ?)")
		search := "%" + query.Search + "%"
		args = append(args, search, search)
	}
	if query.Cursor != "" {
		createdAt, id, err := decodeHistoryCursor(query.Cursor)
		if err != nil {
			return model.HistoryPage{}, err
		}
		where = append(where, "(created_at < ? OR (created_at = ? AND id < ?))")
		args = append(args, createdAt, createdAt, id)
	}
	args = append(args, limit+1)
	sqlQuery := `SELECT id, workspace_id, method, url, status, duration_ms, size_bytes,
	                    (COALESCE(body_ref, '') != '' OR COALESCE(body_inline, '') != '') AS has_body,
	                    created_at
	             FROM history WHERE ` + strings.Join(where, " AND ") + `
	             ORDER BY created_at DESC, id DESC LIMIT ?`
	if query.Cursor == "" && query.Offset > 0 {
		sqlQuery += " OFFSET ?"
		args = append(args, query.Offset)
	}
	rows, err := s.db.Query(sqlQuery, args...)
	if err != nil {
		return model.HistoryPage{}, err
	}
	defer rows.Close()
	items := make([]model.HistorySummary, 0, limit+1)
	for rows.Next() {
		var item model.HistorySummary
		if err := rows.Scan(&item.Id, &item.WorkspaceId, &item.Method, &item.Url, &item.Status,
			&item.DurationMs, &item.SizeBytes, &item.HasBody, &item.CreatedAt); err != nil {
			return model.HistoryPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return model.HistoryPage{}, err
	}
	page := model.HistoryPage{Items: items}
	if len(items) > limit {
		page.HasMore = true
		page.Items = items[:limit]
		last := page.Items[len(page.Items)-1]
		page.NextCursor = encodeHistoryCursor(last.CreatedAt, last.Id)
	}
	return page, nil
}

// GetHistory loads one detail record and enforces workspace ownership.
func (s *Store) GetHistory(workspaceId, id string) (model.HistoryDetail, error) {
	row := s.db.QueryRow(`
		SELECT id, workspace_id, request_snap, status, duration_ms, size_bytes,
		       response_meta, body_ref, body_inline, test_results, created_at
		FROM history WHERE id = ? AND workspace_id = ?`, id, workspaceId)
	var item model.HistoryDetail
	var snapshot, meta string
	var bodyRef, bodyInline, tests sql.NullString
	if err := row.Scan(&item.Id, &item.WorkspaceId, &snapshot, &item.Status, &item.DurationMs,
		&item.SizeBytes, &meta, &bodyRef, &bodyInline, &tests, &item.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return item, errors.New("history item not found in workspace")
		}
		return item, err
	}
	if err := json.Unmarshal([]byte(snapshot), &item.RequestSnap); err != nil {
		return item, err
	}
	var response responseMeta
	if err := json.Unmarshal([]byte(meta), &response); err != nil {
		return item, err
	}
	item.RespHeaders = response.Headers
	item.Timing = response.Timing
	item.BodyRef = bodyRef.String
	item.BodyInline = bodyInline.String
	if tests.Valid && tests.String != "" {
		if err := json.Unmarshal([]byte(tests.String), &item.TestResults); err != nil {
			return item, err
		}
	}
	return item, nil
}

func encodeHistoryCursor(createdAt int64, id string) string {
	raw := strconv.FormatInt(createdAt, 10) + ":" + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeHistoryCursor(cursor string) (int64, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, "", fmt.Errorf("invalid history cursor: %w", err)
	}
	parts := strings.SplitN(string(raw), ":", 2)
	if len(parts) != 2 || parts[1] == "" {
		return 0, "", errors.New("invalid history cursor")
	}
	createdAt, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, "", errors.New("invalid history cursor")
	}
	return createdAt, parts[1], nil
}

// ClearHistory deletes workspace history and associated response blobs.
func (s *Store) ClearHistory(workspaceId string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.Query("SELECT body_ref FROM history WHERE workspace_id = ? AND body_ref != ''", workspaceId)
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
	if _, err := tx.Exec("DELETE FROM history WHERE workspace_id = ?", workspaceId); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for _, ref := range refs {
		s.removeBlobFile(ref)
	}
	return nil
}
