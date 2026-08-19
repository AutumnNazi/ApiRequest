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

const historyResponseRedactionMigrationKey = "history.response-redaction.v1"
const historyRequestRedactionMigrationKey = "history.request-redaction.v2"
const historyResponseBlobRemovalMigrationKey = "history.response-blobs.removed.v1"
const historyResponseLegacyBlobRefsKey = "history.response-blobs.legacy-refs.v1"

// historyRetentionLimit bounds each workspace independently. Stable ordering by
// created_at and id matches ListHistory cursor semantics.
const historyRetentionLimit = 1000

// migrateHistoryResponseSecrets removes structured response credentials written by older builds.
// Malformed legacy metadata is left untouched so one corrupt history row cannot block startup.
func (s *Store) migrateHistoryResponseSecrets() error {
	done, err := s.GetSetting(historyResponseRedactionMigrationKey)
	if err != nil || done == "1" {
		return err
	}
	rows, err := s.db.Query("SELECT id, COALESCE(response_meta, '') FROM history")
	if err != nil {
		return err
	}
	type update struct{ id, meta string }
	updates := []update{}
	redactor := secrets.NewRedactor(nil)
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			rows.Close()
			return err
		}
		var meta responseMeta
		if raw == "" || json.Unmarshal([]byte(raw), &meta) != nil {
			continue
		}
		redactedHeaders := redactor.ResponseHeaders(meta.Headers)
		changed := false
		for i := range meta.Headers {
			if meta.Headers[i].Value != redactedHeaders[i].Value {
				changed = true
				break
			}
		}
		if !changed {
			continue
		}
		meta.Headers = redactedHeaders
		data, err := json.Marshal(meta)
		if err != nil {
			rows.Close()
			return err
		}
		updates = append(updates, update{id: id, meta: string(data)})
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
		if _, err := tx.Exec("UPDATE history SET response_meta = ? WHERE id = ?", item.meta, item.id); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`
		INSERT INTO setting(key, value) VALUES (?, '1')
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, historyResponseRedactionMigrationKey); err != nil {
		return err
	}
	return tx.Commit()
}

// migrateHistoryRequestSecrets removes credentials from legacy request
// snapshots and any response audit fields that echoed those values.
func (s *Store) migrateHistoryRequestSecrets() error {
	done, err := s.GetSetting(historyRequestRedactionMigrationKey)
	if err != nil || done == "1" {
		return err
	}
	rows, err := s.db.Query(`SELECT id, request_snap, COALESCE(response_meta, ''),
		COALESCE(body_inline, ''), COALESCE(test_results, '') FROM history`)
	if err != nil {
		return err
	}
	type update struct{ id, request, meta, body, tests string }
	updates := []update{}
	pendingUnlock := false
	for rows.Next() {
		var item update
		if err := rows.Scan(&item.id, &item.request, &item.meta, &item.body, &item.tests); err != nil {
			rows.Close()
			return err
		}
		var request model.HttpRequest
		if json.Unmarshal([]byte(item.request), &request) != nil {
			continue
		}
		values, err := secrets.StoredRequestCredentialValues(s.vault, request)
		if errors.Is(err, secrets.ErrLocked) {
			pendingUnlock = true
			continue
		}
		if err != nil {
			rows.Close()
			return err
		}
		var meta responseMeta
		if item.meta != "" {
			if json.Unmarshal([]byte(item.meta), &meta) == nil {
				values = append(values, secrets.HeaderValues(meta.Headers)...)
			}
		}
		redactor := secrets.NewRedactor(s.vault, values...)
		request = redactor.Request(request)
		requestData, err := json.Marshal(request)
		if err != nil {
			rows.Close()
			return err
		}
		item.request = string(requestData)
		item.body = redactor.String(item.body)
		if item.meta != "" && meta.Headers != nil {
			meta.Headers = redactor.ResponseHeaders(meta.Headers)
			data, err := json.Marshal(meta)
			if err != nil {
				rows.Close()
				return err
			}
			item.meta = string(data)
		}
		if item.tests != "" {
			var tests []model.TestResult
			if json.Unmarshal([]byte(item.tests), &tests) == nil {
				data, err := json.Marshal(redactor.TestResults(tests))
				if err != nil {
					rows.Close()
					return err
				}
				item.tests = string(data)
			}
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
		if _, err := tx.Exec(`UPDATE history SET request_snap = ?, response_meta = NULLIF(?, ''),
			body_inline = NULLIF(?, ''), test_results = NULLIF(?, '') WHERE id = ?`,
			item.request, item.meta, item.body, item.tests, item.id); err != nil {
			return err
		}
	}
	if !pendingUnlock {
		if _, err := tx.Exec(`
			INSERT INTO setting(key, value) VALUES (?, '1')
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, historyRequestRedactionMigrationKey); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// migrateHistoryResponseBlobs removes legacy large-body references. History is
// currently a request replay audit surface; retaining raw response files there
// bypasses response-secret redaction and serves no user-visible workflow.
func (s *Store) migrateHistoryResponseBlobs() error {
	done, err := s.GetSetting(historyResponseBlobRemovalMigrationKey)
	if err != nil || done == "1" {
		return err
	}
	legacyRaw, err := s.GetSetting(historyResponseLegacyBlobRefsKey)
	if err != nil {
		return err
	}
	var refs []string
	if legacyRaw != "" {
		if err := json.Unmarshal([]byte(legacyRaw), &refs); err != nil {
			return fmt.Errorf("decode legacy history blob refs: %w", err)
		}
	}
	rows, err := s.db.Query("SELECT DISTINCT body_ref FROM history WHERE body_ref IS NOT NULL AND body_ref != ''")
	if err != nil {
		return err
	}
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			rows.Close()
			return err
		}
		refs = append(refs, ref)
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
	if _, err := tx.Exec("UPDATE history SET body_ref = NULL WHERE body_ref IS NOT NULL AND body_ref != ''"); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		INSERT INTO setting(key, value) VALUES (?, '1')
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, historyResponseBlobRemovalMigrationKey); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.removeUnreferencedBlobFiles(refs)
	return nil
}

// InsertHistory writes one redacted detail record and returns its generated id.
func (s *Store) InsertHistory(item model.HistoryDetail) (string, error) {
	if item.BodyRef != "" {
		return "", errors.New("history response blob persistence is disabled")
	}
	if item.Id == "" {
		item.Id = newId()
	}
	if item.CreatedAt == 0 {
		item.CreatedAt = nowMs()
	}
	values := secrets.RequestCredentialValues(item.RequestSnap)
	values = append(values, secrets.HeaderValues(item.RespHeaders)...)
	historyRedactor := secrets.NewRedactor(s.vault, values...)
	item.RequestSnap = historyRedactor.Request(item.RequestSnap)
	item.RespHeaders = historyRedactor.ResponseHeaders(item.RespHeaders)
	item.BodyInline = historyRedactor.String(item.BodyInline)
	item.TestResults = historyRedactor.TestResults(item.TestResults)
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
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`
		INSERT INTO history (id, workspace_id, request_snap, method, url, status, duration_ms,
		                     size_bytes, response_meta, body_ref, body_inline, test_results, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		item.Id, item.WorkspaceId, string(snapshot), item.RequestSnap.Method, item.RequestSnap.Url,
		item.Status, item.DurationMs, item.SizeBytes, string(meta),
		sql.NullString{String: item.BodyRef, Valid: item.BodyRef != ""},
		sql.NullString{String: item.BodyInline, Valid: item.BodyInline != ""},
		tests, item.CreatedAt)
	if err != nil {
		return item.Id, err
	}
	prunedRefs, err := pruneHistoryTx(tx, item.WorkspaceId, historyRetentionLimit)
	if err != nil {
		return item.Id, err
	}
	if err := tx.Commit(); err != nil {
		return item.Id, err
	}
	s.removeUnreferencedBlobFiles(prunedRefs)
	return item.Id, nil
}

func pruneHistoryTx(tx *sql.Tx, workspaceId string, limit int) ([]string, error) {
	if limit <= 0 {
		return nil, errors.New("history retention limit must be positive")
	}
	const staleIds = `
		SELECT id FROM history
		WHERE workspace_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT -1 OFFSET ?`
	rows, err := tx.Query(`
		SELECT COALESCE(body_ref, '') FROM history
		WHERE workspace_id = ? AND id IN (`+staleIds+`)`, workspaceId, workspaceId, limit)
	if err != nil {
		return nil, err
	}
	var refs []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			rows.Close()
			return nil, err
		}
		if ref != "" {
			refs = append(refs, ref)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if _, err := tx.Exec(`
		DELETE FROM history
		WHERE workspace_id = ? AND id IN (`+staleIds+`)`, workspaceId, workspaceId, limit); err != nil {
		return nil, err
	}
	return refs, nil
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
		where = append(where, `(method LIKE ? ESCAPE '\' OR url LIKE ? ESCAPE '\')`)
		search := "%" + escapeLikePattern(query.Search) + "%"
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

func escapeLikePattern(value string) string {
	return strings.NewReplacer(
		`\`, `\\`,
		`%`, `\%`,
		`_`, `\_`,
	).Replace(value)
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
	s.removeUnreferencedBlobFiles(refs)
	return nil
}
