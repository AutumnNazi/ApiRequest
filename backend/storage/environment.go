package storage

import (
	"database/sql"
	"encoding/json"
	"errors"

	"apirequest/backend/model"
	"apirequest/backend/secrets"
)

// ListEnvironments 列出工作区全部环境
func (s *Store) ListEnvironments(workspaceId string) ([]model.Environment, error) {
	rows, err := s.db.Query(`
		SELECT id, workspace_id, name, variables, is_active, created_at, updated_at
		FROM environment WHERE workspace_id = ? ORDER BY created_at`, workspaceId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []model.Environment{}
	for rows.Next() {
		var e model.Environment
		var vars string
		var active int
		if err := rows.Scan(&e.Id, &e.WorkspaceId, &e.Name, &vars, &active, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return nil, err
		}
		e.IsActive = active != 0
		if err := json.Unmarshal([]byte(vars), &e.Variables); err != nil {
			return nil, err
		}
		resolved, err := secrets.ResolveVariables(s.vault, e.Variables)
		if err != nil {
			return nil, err
		}
		e.Variables = resolved
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetEnvironment 取单个环境；不存在返回 sql.ErrNoRows
func (s *Store) GetEnvironment(envId string) (model.Environment, error) {
	var e model.Environment
	var vars string
	var active int
	err := s.db.QueryRow(`
		SELECT id, workspace_id, name, variables, is_active, created_at, updated_at
		FROM environment WHERE id = ?`, envId).
		Scan(&e.Id, &e.WorkspaceId, &e.Name, &vars, &active, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return e, err
	}
	e.IsActive = active != 0
	err = json.Unmarshal([]byte(vars), &e.Variables)
	if err == nil {
		e.Variables, err = secrets.ResolveVariables(s.vault, e.Variables)
	}
	return e, err
}

// UpsertEnvironment 新增或更新环境
func (s *Store) UpsertEnvironment(e model.Environment) (model.Environment, error) {
	now := nowMs()
	if e.Id == "" {
		e.Id = newId()
		e.CreatedAt = now
	}
	e.UpdatedAt = now
	if err := s.validateEnvironmentOwnership(e); err != nil {
		return e, err
	}
	if e.Variables == nil {
		e.Variables = []model.Variable{}
	}
	active := 0
	if e.IsActive {
		active = 1
	}
	err := s.withSecretWrite(func(writer secrets.SecretWriter) error {
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
		if err := deleteRemovedSecretReferences(writer, oldRefs, secrets.VariableReferences(protected)); err != nil {
			return err
		}
		_, err = s.db.Exec(`
			INSERT INTO environment (id, workspace_id, name, variables, is_active, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?)
			ON CONFLICT(id) DO UPDATE SET
			  name = excluded.name,
			  variables = excluded.variables,
			  is_active = excluded.is_active,
			  updated_at = excluded.updated_at`,
			e.Id, e.WorkspaceId, e.Name, string(vars), active, e.CreatedAt, e.UpdatedAt)
		return err
	})
	return e, err
}

func (s *Store) validateEnvironmentOwnership(e model.Environment) error {
	var workspaceExists bool
	if err := s.db.QueryRow("SELECT EXISTS(SELECT 1 FROM workspace WHERE id = ?)", e.WorkspaceId).Scan(&workspaceExists); err != nil {
		return err
	}
	if !workspaceExists {
		return errors.New("workspace not found")
	}
	var existingWorkspace string
	err := s.db.QueryRow("SELECT workspace_id FROM environment WHERE id = ?", e.Id).Scan(&existingWorkspace)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if existingWorkspace != e.WorkspaceId {
		return errors.New("environment belongs to a different workspace")
	}
	return nil
}

// DeleteEnvironment 删除环境
func (s *Store) DeleteEnvironment(envId string) error {
	return s.withSecretWrite(func(writer secrets.SecretWriter) error {
		oldRefs, err := s.storedEnvironmentSecretReferences(envId)
		if err != nil {
			return err
		}
		if err := deleteRemovedSecretReferences(writer, oldRefs, nil); err != nil {
			return err
		}
		_, err = s.db.Exec("DELETE FROM environment WHERE id = ?", envId)
		return err
	})
}

// SetActiveEnvironment 激活指定环境（envId 为空 = 全部取消激活，即 No Environment）
func (s *Store) SetActiveEnvironment(workspaceId, envId string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec("UPDATE environment SET is_active = 0 WHERE workspace_id = ?", workspaceId); err != nil {
		return err
	}
	if envId != "" {
		res, err := tx.Exec("UPDATE environment SET is_active = 1 WHERE id = ? AND workspace_id = ?", envId, workspaceId)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return sql.ErrNoRows
		}
	}
	return tx.Commit()
}

// ActiveEnvironment 返回当前激活环境；无则 ok=false
func (s *Store) ActiveEnvironment(workspaceId string) (model.Environment, bool, error) {
	rows, err := s.db.Query(
		"SELECT id FROM environment WHERE workspace_id = ? AND is_active = 1 LIMIT 1", workspaceId)
	if err != nil {
		return model.Environment{}, false, err
	}
	var id string
	found := rows.Next()
	if found {
		rows.Scan(&id)
	}
	rows.Close()
	if !found {
		return model.Environment{}, false, nil
	}
	e, err := s.GetEnvironment(id)
	return e, err == nil, err
}

// GetGlobalVariables 读全局变量（无记录返回空）
func (s *Store) GetGlobalVariables(workspaceId string) ([]model.Variable, error) {
	var vars string
	err := s.db.QueryRow("SELECT variables FROM global_var WHERE workspace_id = ?", workspaceId).Scan(&vars)
	if err == sql.ErrNoRows {
		return []model.Variable{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []model.Variable{}
	err = json.Unmarshal([]byte(vars), &out)
	if err == nil {
		out, err = secrets.ResolveVariables(s.vault, out)
	}
	return out, err
}

// SetGlobalVariables 写全局变量
func (s *Store) SetGlobalVariables(workspaceId string, vars []model.Variable) error {
	return s.writeGlobalVariables(workspaceId, vars, nowMs())
}

// ApplySyncGlobalVariables writes an exact remote revision instead of creating a local timestamp.
func (s *Store) ApplySyncGlobalVariables(workspaceId string, vars []model.Variable, updatedAt int64) error {
	return s.writeGlobalVariables(workspaceId, vars, updatedAt)
}

func (s *Store) writeGlobalVariables(workspaceId string, vars []model.Variable, updatedAt int64) error {
	if vars == nil {
		vars = []model.Variable{}
	}
	return s.withSecretWrite(func(writer secrets.SecretWriter) error {
		oldRefs, err := s.storedGlobalSecretReferences(workspaceId)
		if err != nil {
			return err
		}
		protected, err := protectVariables(writer, vars, "workspace/"+workspaceId+"/globals")
		if err != nil {
			return err
		}
		b, err := json.Marshal(protected)
		if err != nil {
			return err
		}
		if err := deleteRemovedSecretReferences(writer, oldRefs, secrets.VariableReferences(protected)); err != nil {
			return err
		}
		_, err = s.db.Exec(`
			INSERT INTO global_var (workspace_id, variables, updated_at) VALUES (?,?,?)
			ON CONFLICT(workspace_id) DO UPDATE SET
			  variables = excluded.variables,
			  updated_at = excluded.updated_at`,
			workspaceId, string(b), updatedAt)
		return err
	})
}

// GlobalVariablesRevision returns zero when a workspace has no globals row.
func (s *Store) GlobalVariablesRevision(workspaceId string) (int64, error) {
	var updatedAt int64
	err := s.db.QueryRow("SELECT updated_at FROM global_var WHERE workspace_id = ?", workspaceId).Scan(&updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return updatedAt, err
}

// NodeAncestors 返回节点自身到集合根的链（自身在前，根在后）。
// 用于变量与脚本的继承合并。
func (s *Store) NodeAncestors(nodeId string) ([]model.Node, error) {
	rows, err := s.db.Query(`
		WITH RECURSIVE chain(id, workspace_id, parent_id, kind, name, sort_order,
		                     request_data, auth, variables, pre_script, test_script,
		                     created_at, updated_at, depth) AS (
		  SELECT id, workspace_id, parent_id, kind, name, sort_order,
		         request_data, auth, variables, pre_script, test_script,
		         created_at, updated_at, 0
		  FROM node WHERE id = ? AND deleted_at IS NULL
		  UNION ALL
		  SELECT n.id, n.workspace_id, n.parent_id, n.kind, n.name, n.sort_order,
		         n.request_data, n.auth, n.variables, n.pre_script, n.test_script,
		         n.created_at, n.updated_at, chain.depth + 1
		  FROM node n JOIN chain ON n.id = chain.parent_id
		  WHERE n.deleted_at IS NULL
		)
		SELECT id, workspace_id, parent_id, kind, name, sort_order,
		       request_data, auth, variables, pre_script, test_script,
		       created_at, updated_at
		FROM chain ORDER BY depth`, nodeId)
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
		if err := s.hydrateNode(&n, &r); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// NodeAncestorsInWorkspace returns the inheritance chain only when the leaf belongs to workspaceId.
func (s *Store) NodeAncestorsInWorkspace(nodeId, workspaceId string) ([]model.Node, error) {
	belongs, err := s.NodeBelongsToWorkspace(nodeId, workspaceId)
	if err != nil {
		return nil, err
	}
	if !belongs {
		return nil, errors.New("request node belongs to a different workspace or does not exist")
	}
	chain, err := s.NodeAncestors(nodeId)
	if err != nil {
		return nil, err
	}
	for _, node := range chain {
		if node.WorkspaceId != workspaceId {
			return nil, errors.New("node ancestry crosses workspace boundary")
		}
	}
	return chain, nil
}
