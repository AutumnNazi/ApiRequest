package storage

import (
	"database/sql"
	"encoding/json"

	"apirequest/backend/model"
	"apirequest/backend/runner"
)

const runnerRunsDefaultLimit = 20

// runnerRunRetentionLimit 每 (workspace, collection) 保留的最近运行次数；
// results JSON 体积可观，不设上限会随使用无限增长（同 history 保留策略思路）
const runnerRunRetentionLimit = 200

// SaveRunnerRun 落库一次运行报告（按 runId 幂等覆盖，重试不产生重复行）
func (s *Store) SaveRunnerRun(workspaceId, collectionId string, report *runner.Report) error {
	if report == nil || report.RunId == "" {
		return model.NewError(model.KindValidation, "runner report needs a runId")
	}
	results, err := json.Marshal(report.Results)
	if err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	createdAt := report.CreatedAt
	if createdAt == 0 {
		createdAt = nowMs()
	}
	canceled := 0
	if report.Canceled {
		canceled = 1
	}
	_, err = s.db.Exec(`
		INSERT INTO runner_run (id, workspace_id, collection_id, created_at, total, passed, failed,
		                        skipped, duration_ms, canceled, results)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		  collection_id = excluded.collection_id,
		  created_at = excluded.created_at,
		  total = excluded.total, passed = excluded.passed, failed = excluded.failed,
		  skipped = excluded.skipped, duration_ms = excluded.duration_ms,
		  canceled = excluded.canceled, results = excluded.results`,
		report.RunId, workspaceId, collectionId, createdAt, report.Total, report.Passed, report.Failed,
		report.Skipped, report.DurationMs, canceled, string(results))
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`
		DELETE FROM runner_run
		WHERE workspace_id = ? AND collection_id = ?
		  AND id NOT IN (
		    SELECT id FROM runner_run
		    WHERE workspace_id = ? AND collection_id = ?
		    ORDER BY created_at DESC, id DESC
		    LIMIT ?)`,
		workspaceId, collectionId, workspaceId, collectionId, s.runnerRunRetention())
	return err
}

// ListRunnerRuns 按时间倒序列出运行摘要；列表投影不携带 results 明细
func (s *Store) ListRunnerRuns(workspaceId string, query model.RunnerRunQuery) (model.RunnerRunPage, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = runnerRunsDefaultLimit
	}
	where := "workspace_id = ?"
	args := []any{workspaceId}
	if query.CollectionId != "" {
		where += " AND collection_id = ?"
		args = append(args, query.CollectionId)
	}
	if query.Cursor != "" {
		createdAt, id, err := decodeHistoryCursor(query.Cursor)
		if err != nil {
			return model.RunnerRunPage{}, model.NewError(model.KindValidation, "invalid runner run cursor")
		}
		where += " AND (created_at, id) < (?, ?)"
		args = append(args, createdAt, id)
	}
	rows, err := s.db.Query(`
		SELECT id, collection_id, created_at, total, passed, failed, skipped, duration_ms, canceled
		FROM runner_run WHERE `+where+`
		ORDER BY created_at DESC, id DESC LIMIT ?`, append(args, limit+1)...)
	if err != nil {
		return model.RunnerRunPage{}, err
	}
	defer rows.Close()
	page := model.RunnerRunPage{Items: []model.RunnerRunSummary{}}
	for rows.Next() {
		var (
			item     model.RunnerRunSummary
			canceled int
		)
		if err := rows.Scan(&item.RunId, &item.CollectionId, &item.CreatedAt, &item.Total,
			&item.Passed, &item.Failed, &item.Skipped, &item.DurationMs, &canceled); err != nil {
			return model.RunnerRunPage{}, err
		}
		item.Canceled = canceled != 0
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return model.RunnerRunPage{}, err
	}
	if len(page.Items) > limit {
		page.Items = page.Items[:limit]
		page.HasMore = true
		last := page.Items[len(page.Items)-1]
		page.NextCursor = encodeHistoryCursor(last.CreatedAt, last.RunId)
	}
	return page, nil
}

// GetRunnerRun 取单次运行的完整报告（含明细）；不存在或跨工作区返回错误
func (s *Store) GetRunnerRun(workspaceId, runId string) (*runner.Report, error) {
	var (
		r        runner.Report
		canceled int
		results  string
	)
	err := s.db.QueryRow(`
		SELECT id, created_at, total, passed, failed, skipped, duration_ms, canceled, results
		FROM runner_run WHERE workspace_id = ? AND id = ?`, workspaceId, runId).
		Scan(&r.RunId, &r.CreatedAt, &r.Total, &r.Passed, &r.Failed,
			&r.Skipped, &r.DurationMs, &canceled, &results)
	if err == sql.ErrNoRows {
		return nil, model.NewError(model.KindValidation, "no runner run: "+runId)
	}
	if err != nil {
		return nil, err
	}
	r.Canceled = canceled != 0
	r.Results = []runner.RequestResult{}
	if err := json.Unmarshal([]byte(results), &r.Results); err != nil {
		return nil, model.WrapError(model.KindStorage, err)
	}
	return &r, nil
}

// DeleteRunnerRun 删除单次运行
func (s *Store) DeleteRunnerRun(workspaceId, runId string) error {
	res, err := s.db.Exec(`DELETE FROM runner_run WHERE workspace_id = ? AND id = ?`, workspaceId, runId)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return model.NewError(model.KindValidation, "no runner run: "+runId)
	}
	return nil
}

// ClearRunnerRuns 清空工作区全部运行历史
func (s *Store) ClearRunnerRuns(workspaceId string) error {
	_, err := s.db.Exec(`DELETE FROM runner_run WHERE workspace_id = ?`, workspaceId)
	return err
}
