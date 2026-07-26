package storage

import (
	"database/sql"
	"encoding/json"

	"apirequest/backend/model"
)

// SyncNodeRow 同步用节点行（含墓碑）
type SyncNodeRow struct {
	Node      model.Node
	DeletedAt int64
}

// ListNodesForSync 返回工作区全部节点（含软删除墓碑，同步传播删除用）
func (s *Store) ListNodesForSync(workspaceId string) ([]SyncNodeRow, error) {
	rows, err := s.db.Query(`
		SELECT id, workspace_id, parent_id, kind, name, sort_order,
		       request_data, auth, variables, pre_script, test_script,
		       created_at, updated_at, deleted_at
		FROM node WHERE workspace_id = ?`, workspaceId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []SyncNodeRow{}
	for rows.Next() {
		var n model.Node
		var r nodeRow
		var deletedAt sql.NullInt64
		if err := rows.Scan(&n.Id, &n.WorkspaceId, &r.parentId, &n.Kind, &n.Name, &n.SortOrder,
			&r.requestData, &r.auth, &r.variables, &r.preScript, &r.testScript,
			&n.CreatedAt, &n.UpdatedAt, &deletedAt); err != nil {
			return nil, err
		}
		if err := hydrateNode(&n, &r); err != nil {
			return nil, err
		}
		out = append(out, SyncNodeRow{Node: n, DeletedAt: deletedAt.Int64})
	}
	return out, rows.Err()
}

// EnsureWorkspace 确保指定 id 的工作区存在（同步拉取远端工作区时用）
func (s *Store) EnsureWorkspace(id, name string) error {
	if name == "" {
		name = "Synced Workspace"
	}
	now := nowMs()
	_, err := s.db.Exec(`
		INSERT INTO workspace (id, name, type, created_at, updated_at)
		VALUES (?,?, 'local', ?, ?)
		ON CONFLICT(id) DO NOTHING`, id, name, now, now)
	return err
}

// ApplySyncNode 原样写入节点（保留 id/时间戳/墓碑；不走 UpsertNode 的时间戳刷新）
func (s *Store) ApplySyncNode(row SyncNodeRow) error {
	n := row.Node
	var reqJSON, authJSON, varsJSON sql.NullString
	if n.Request != nil {
		b, err := json.Marshal(n.Request)
		if err != nil {
			return err
		}
		reqJSON = sql.NullString{String: string(b), Valid: true}
	}
	if n.Auth != nil {
		b, err := json.Marshal(n.Auth)
		if err != nil {
			return err
		}
		authJSON = sql.NullString{String: string(b), Valid: true}
	}
	if len(n.Variables) > 0 {
		b, err := json.Marshal(n.Variables)
		if err != nil {
			return err
		}
		varsJSON = sql.NullString{String: string(b), Valid: true}
	}
	_, err := s.db.Exec(`
		INSERT INTO node (id, workspace_id, parent_id, kind, name, sort_order,
		                  request_data, auth, variables, pre_script, test_script,
		                  created_at, updated_at, deleted_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		  workspace_id = excluded.workspace_id,
		  parent_id = excluded.parent_id,
		  name = excluded.name,
		  sort_order = excluded.sort_order,
		  request_data = excluded.request_data,
		  auth = excluded.auth,
		  variables = excluded.variables,
		  pre_script = excluded.pre_script,
		  test_script = excluded.test_script,
		  updated_at = excluded.updated_at,
		  deleted_at = excluded.deleted_at`,
		n.Id, n.WorkspaceId,
		sql.NullString{String: n.ParentId, Valid: n.ParentId != ""},
		n.Kind, n.Name, n.SortOrder,
		reqJSON, authJSON, varsJSON,
		sql.NullString{String: n.PreScript, Valid: n.PreScript != ""},
		sql.NullString{String: n.TestScript, Valid: n.TestScript != ""},
		n.CreatedAt, n.UpdatedAt,
		sql.NullInt64{Int64: row.DeletedAt, Valid: row.DeletedAt > 0})
	return err
}

// ApplySyncEnvironment 原样写入环境（保留 id 与时间戳）
func (s *Store) ApplySyncEnvironment(e model.Environment) error {
	vars, err := json.Marshal(e.Variables)
	if err != nil {
		return err
	}
	active := 0
	if e.IsActive {
		active = 1
	}
	_, err = s.db.Exec(`
		INSERT INTO environment (id, workspace_id, name, variables, is_active, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
		  name = excluded.name,
		  variables = excluded.variables,
		  updated_at = excluded.updated_at`,
		e.Id, e.WorkspaceId, e.Name, string(vars), active, e.CreatedAt, e.UpdatedAt)
	// is_active 不同步覆盖：激活状态是本机 UI 状态
	return err
}
