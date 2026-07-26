package storage

import (
	"database/sql"
	"encoding/json"

	"apirequest/backend/model"
)

// nodeRow 与 node 表列一一对应的中间结构
type nodeRow struct {
	requestData sql.NullString
	auth        sql.NullString
	variables   sql.NullString
	preScript   sql.NullString
	testScript  sql.NullString
	parentId    sql.NullString
}

// ListNodes 返回工作区下全部未删除节点（前端组树）
func (s *Store) ListNodes(workspaceId string) ([]model.Node, error) {
	rows, err := s.db.Query(`
		SELECT id, workspace_id, parent_id, kind, name, sort_order,
		       request_data, auth, variables, pre_script, test_script,
		       created_at, updated_at
		FROM node
		WHERE workspace_id = ? AND deleted_at IS NULL
		ORDER BY sort_order, created_at`, workspaceId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.Node{}
	for rows.Next() {
		var n model.Node
		var r nodeRow
		if err := rows.Scan(&n.Id, &n.WorkspaceId, &r.parentId, &n.Kind, &n.Name, &n.SortOrder,
			&r.requestData, &r.auth, &r.variables, &r.preScript, &r.testScript,
			&n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		if err := hydrateNode(&n, &r); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func hydrateNode(n *model.Node, r *nodeRow) error {
	n.ParentId = r.parentId.String
	n.PreScript = r.preScript.String
	n.TestScript = r.testScript.String
	if r.requestData.Valid && r.requestData.String != "" {
		var req model.HttpRequest
		if err := json.Unmarshal([]byte(r.requestData.String), &req); err != nil {
			return err
		}
		n.Request = &req
	}
	if r.auth.Valid && r.auth.String != "" {
		var a model.Auth
		if err := json.Unmarshal([]byte(r.auth.String), &a); err != nil {
			return err
		}
		n.Auth = &a
	}
	if r.variables.Valid && r.variables.String != "" {
		if err := json.Unmarshal([]byte(r.variables.String), &n.Variables); err != nil {
			return err
		}
	}
	return nil
}

// UpsertNode 新增或更新节点；新节点自动分配 id 与时间戳
func (s *Store) UpsertNode(n model.Node) (model.Node, error) {
	now := nowMs()
	if n.Id == "" {
		n.Id = newId()
		n.CreatedAt = now
	}
	n.UpdatedAt = now

	var reqJSON, authJSON, varsJSON sql.NullString
	if n.Request != nil {
		b, err := json.Marshal(n.Request)
		if err != nil {
			return n, err
		}
		reqJSON = sql.NullString{String: string(b), Valid: true}
	}
	if n.Auth != nil {
		b, err := json.Marshal(n.Auth)
		if err != nil {
			return n, err
		}
		authJSON = sql.NullString{String: string(b), Valid: true}
	}
	if len(n.Variables) > 0 {
		b, err := json.Marshal(n.Variables)
		if err != nil {
			return n, err
		}
		varsJSON = sql.NullString{String: string(b), Valid: true}
	}
	parentId := sql.NullString{String: n.ParentId, Valid: n.ParentId != ""}

	_, err := s.db.Exec(`
		INSERT INTO node (id, workspace_id, parent_id, kind, name, sort_order,
		                  request_data, auth, variables, pre_script, test_script,
		                  created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		  parent_id = excluded.parent_id,
		  name = excluded.name,
		  sort_order = excluded.sort_order,
		  request_data = excluded.request_data,
		  auth = excluded.auth,
		  variables = excluded.variables,
		  pre_script = excluded.pre_script,
		  test_script = excluded.test_script,
		  updated_at = excluded.updated_at`,
		n.Id, n.WorkspaceId, parentId, n.Kind, n.Name, n.SortOrder,
		reqJSON, authJSON, varsJSON,
		sql.NullString{String: n.PreScript, Valid: n.PreScript != ""},
		sql.NullString{String: n.TestScript, Valid: n.TestScript != ""},
		n.CreatedAt, n.UpdatedAt)
	return n, err
}

// DeleteNode 软删除节点及其全部后代
func (s *Store) DeleteNode(nodeId string) error {
	_, err := s.db.Exec(`
		WITH RECURSIVE sub(id) AS (
		  SELECT id FROM node WHERE id = ?
		  UNION ALL
		  SELECT n.id FROM node n JOIN sub ON n.parent_id = sub.id
		)
		UPDATE node SET deleted_at = ? WHERE id IN (SELECT id FROM sub)`,
		nodeId, nowMs())
	return err
}

// MoveNode 改父与排序
func (s *Store) MoveNode(nodeId, newParentId string, sortOrder float64) error {
	parentId := sql.NullString{String: newParentId, Valid: newParentId != ""}
	_, err := s.db.Exec(
		"UPDATE node SET parent_id = ?, sort_order = ?, updated_at = ? WHERE id = ?",
		parentId, sortOrder, nowMs(), nodeId)
	return err
}
