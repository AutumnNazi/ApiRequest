package storage

import (
	"database/sql"
	"encoding/json"
	"errors"

	"apirequest/backend/model"
	"apirequest/backend/secrets"
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
		if err := s.hydrateNode(&n, &r); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) hydrateNode(n *model.Node, r *nodeRow) error {
	n.ParentId = r.parentId.String
	n.PreScript = r.preScript.String
	n.TestScript = r.testScript.String
	if r.requestData.Valid && r.requestData.String != "" {
		var req model.HttpRequest
		if err := json.Unmarshal([]byte(r.requestData.String), &req); err != nil {
			return err
		}
		resolved, err := secrets.ResolveRequest(s.vault, req)
		if err != nil {
			return err
		}
		req = resolved
		n.Request = &req
	}
	if r.auth.Valid && r.auth.String != "" {
		var a model.Auth
		if err := json.Unmarshal([]byte(r.auth.String), &a); err != nil {
			return err
		}
		resolved, err := secrets.ResolveAuth(s.vault, a)
		if err != nil {
			return err
		}
		a = resolved
		n.Auth = &a
	}
	if r.variables.Valid && r.variables.String != "" {
		if err := json.Unmarshal([]byte(r.variables.String), &n.Variables); err != nil {
			return err
		}
		resolved, err := secrets.ResolveVariables(s.vault, n.Variables)
		if err != nil {
			return err
		}
		n.Variables = resolved
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
	if err := s.validateNodeOwnership(n); err != nil {
		return n, err
	}
	err := s.withSecretWrite(func(writer secrets.SecretWriter) error {
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
		parentId := sql.NullString{String: n.ParentId, Valid: n.ParentId != ""}
		if err := deleteRemovedSecretReferences(writer, oldRefs, secrets.NodeReferences(stored)); err != nil {
			return err
		}
		_, err = s.db.Exec(`
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
		return err
	})
	return n, err
}

func (s *Store) validateNodeOwnership(n model.Node) error {
	var workspaceExists bool
	if err := s.db.QueryRow("SELECT EXISTS(SELECT 1 FROM workspace WHERE id = ?)", n.WorkspaceId).Scan(&workspaceExists); err != nil {
		return err
	}
	if !workspaceExists {
		return errors.New("workspace not found")
	}
	var existingWorkspace, existingKind string
	err := s.db.QueryRow("SELECT workspace_id, kind FROM node WHERE id = ?", n.Id).Scan(&existingWorkspace, &existingKind)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		if existingWorkspace != n.WorkspaceId {
			return errors.New("node belongs to a different workspace")
		}
		if existingKind != n.Kind {
			return errors.New("node kind cannot be changed")
		}
	}
	if n.Kind == "collection" {
		if n.ParentId != "" {
			return errors.New("collection must stay at root")
		}
		return nil
	}
	if n.ParentId == "" {
		return errors.New("folder and request nodes require a parent")
	}
	if n.ParentId == n.Id {
		return errors.New("node cannot be its own parent")
	}
	var parentWorkspace, parentKind string
	if err := s.db.QueryRow("SELECT workspace_id, kind FROM node WHERE id = ? AND deleted_at IS NULL", n.ParentId).Scan(&parentWorkspace, &parentKind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("parent node not found")
		}
		return err
	}
	if parentWorkspace != n.WorkspaceId {
		return errors.New("parent must belong to the same workspace")
	}
	if parentKind != "collection" && parentKind != "folder" {
		return errors.New("parent must be a collection or folder")
	}
	return nil
}

// NodeBelongsToWorkspace verifies ownership without hydrating credential fields.
func (s *Store) NodeBelongsToWorkspace(nodeId, workspaceId string) (bool, error) {
	var belongs bool
	err := s.db.QueryRow(
		"SELECT EXISTS(SELECT 1 FROM node WHERE id = ? AND workspace_id = ? AND deleted_at IS NULL)",
		nodeId, workspaceId,
	).Scan(&belongs)
	return belongs, err
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

// MoveNode 改父与排序，并保证树不会跨工作区、出现无效父级或循环引用。
func (s *Store) MoveNode(nodeId, newParentId string, sortOrder float64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var workspaceId, kind string
	if err := tx.QueryRow("SELECT workspace_id, kind FROM node WHERE id = ? AND deleted_at IS NULL", nodeId).Scan(&workspaceId, &kind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("node not found")
		}
		return err
	}
	if kind == "collection" && newParentId != "" {
		return errors.New("collection must stay at root")
	}
	if kind != "collection" && newParentId == "" {
		return errors.New("only collections may stay at root")
	}

	parentId := sql.NullString{String: newParentId, Valid: newParentId != ""}
	if newParentId != "" {
		var parentWorkspace, parentKind string
		if err := tx.QueryRow("SELECT workspace_id, kind FROM node WHERE id = ? AND deleted_at IS NULL", newParentId).Scan(&parentWorkspace, &parentKind); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("parent node not found")
			}
			return err
		}
		if parentWorkspace != workspaceId {
			return errors.New("parent must belong to the same workspace")
		}
		if parentKind != "collection" && parentKind != "folder" {
			return errors.New("parent must be a collection or folder")
		}
		var createsCycle bool
		if err := tx.QueryRow(`WITH RECURSIVE sub(id) AS (
			SELECT id FROM node WHERE id = ? AND deleted_at IS NULL
			UNION ALL
			SELECT n.id FROM node n JOIN sub ON n.parent_id = sub.id WHERE n.deleted_at IS NULL
		) SELECT EXISTS(SELECT 1 FROM sub WHERE id = ?)`, nodeId, newParentId).Scan(&createsCycle); err != nil {
			return err
		}
		if createsCycle {
			return errors.New("cannot move a node into itself or its descendant")
		}
	}
	if _, err := tx.Exec(
		"UPDATE node SET parent_id = ?, sort_order = ?, updated_at = ? WHERE id = ?",
		parentId, sortOrder, nowMs(), nodeId); err != nil {
		return err
	}
	return tx.Commit()
}
