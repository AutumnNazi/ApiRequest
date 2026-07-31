package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"apirequest/backend/model"
	"apirequest/backend/secrets"
)

// SyncNodeRow 同步用节点行（含墓碑）
type SyncNodeRow struct {
	Node      model.Node
	DeletedAt int64
}

// ValidateSyncOwnership rejects entity ID collisions before a merged snapshot
// writes anything. Parent topology is validated by the sync package itself.
func (s *Store) ValidateSyncOwnership(workspaceId string, nodes []SyncNodeRow, environments []model.Environment) error {
	for _, row := range nodes {
		var existingWorkspace, existingKind string
		err := s.db.QueryRow("SELECT workspace_id, kind FROM node WHERE id = ?", row.Node.Id).
			Scan(&existingWorkspace, &existingKind)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			if existingWorkspace != workspaceId {
				return fmt.Errorf("sync node %q belongs to a different workspace", row.Node.Id)
			}
			if existingKind != row.Node.Kind {
				return fmt.Errorf("sync node %q kind cannot be changed", row.Node.Id)
			}
		}
	}
	for _, environment := range environments {
		var existingWorkspace string
		err := s.db.QueryRow("SELECT workspace_id FROM environment WHERE id = ?", environment.Id).
			Scan(&existingWorkspace)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil && existingWorkspace != workspaceId {
			return fmt.Errorf("sync environment %q belongs to a different workspace", environment.Id)
		}
	}
	return nil
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
		if err := s.hydrateNode(&n, &r); err != nil {
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
	if err := s.validateSyncNodeOwnership(n); err != nil {
		return err
	}
	return s.withSecretWrite(func(writer secrets.SecretWriter) error {
		oldRefs, err := s.storedNodeSecretReferences(n.Id)
		if err != nil {
			return err
		}
		stored, err := protectNode(writer, n)
		if err != nil {
			return err
		}
		var reqJSON, authJSON, varsJSON sql.NullString
		if stored.Request != nil {
			b, err := json.Marshal(stored.Request)
			if err != nil {
				return err
			}
			reqJSON = sql.NullString{String: string(b), Valid: true}
		}
		if stored.Auth != nil {
			b, err := json.Marshal(stored.Auth)
			if err != nil {
				return err
			}
			authJSON = sql.NullString{String: string(b), Valid: true}
		}
		if len(stored.Variables) > 0 {
			b, err := json.Marshal(stored.Variables)
			if err != nil {
				return err
			}
			varsJSON = sql.NullString{String: string(b), Valid: true}
		}
		if err := deleteRemovedSecretReferences(writer, oldRefs, secrets.NodeReferences(stored)); err != nil {
			return err
		}
		_, err = s.db.Exec(`
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
	})
}

func (s *Store) validateSyncNodeOwnership(n model.Node) error {
	var workspaceExists bool
	if err := s.db.QueryRow("SELECT EXISTS(SELECT 1 FROM workspace WHERE id = ?)", n.WorkspaceId).Scan(&workspaceExists); err != nil {
		return err
	}
	if !workspaceExists {
		return errors.New("sync workspace not found")
	}
	var existingWorkspace, existingKind string
	err := s.db.QueryRow("SELECT workspace_id, kind FROM node WHERE id = ?", n.Id).Scan(&existingWorkspace, &existingKind)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if existingWorkspace != n.WorkspaceId {
			return errors.New("sync node belongs to a different workspace")
		}
		if existingKind != n.Kind {
			return errors.New("sync node kind cannot be changed")
		}
	}
	if n.Kind == "collection" {
		if n.ParentId != "" {
			return errors.New("sync collection must stay at root")
		}
		return nil
	}
	if n.Kind != "folder" && n.Kind != "request" {
		return errors.New("sync node kind is invalid")
	}
	if n.ParentId == "" {
		return errors.New("sync folder and request nodes require a parent")
	}
	if n.ParentId == n.Id {
		return errors.New("sync node cannot be its own parent")
	}
	var parentWorkspace, parentKind string
	if err := s.db.QueryRow("SELECT workspace_id, kind FROM node WHERE id = ?", n.ParentId).Scan(&parentWorkspace, &parentKind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("sync parent node not found")
		}
		return err
	}
	if parentWorkspace != n.WorkspaceId {
		return errors.New("sync parent must belong to the same workspace")
	}
	if parentKind != "collection" && parentKind != "folder" {
		return errors.New("sync parent must be a collection or folder")
	}
	return nil
}

// ApplySyncEnvironment 原样写入环境（保留 id 与时间戳）
func (s *Store) ApplySyncEnvironment(e model.Environment) error {
	var existingWorkspace string
	if err := s.db.QueryRow("SELECT workspace_id FROM environment WHERE id = ?", e.Id).Scan(&existingWorkspace); err == nil {
		if existingWorkspace != e.WorkspaceId {
			return errors.New("sync environment belongs to a different workspace")
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	return s.withSecretWrite(func(writer secrets.SecretWriter) error {
		oldRefs, err := s.storedEnvironmentSecretReferences(e.Id)
		if err != nil {
			return err
		}
		protected, err := protectVariables(writer, e.Variables, "environment/"+e.Id)
		if err != nil {
			return err
		}
		vars, err := json.Marshal(protected)
		if err != nil {
			return err
		}
		active := 0 // Environment activation is local UI state and never imported.
		if err := deleteRemovedSecretReferences(writer, oldRefs, secrets.VariableReferences(protected)); err != nil {
			return err
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
	})
}
