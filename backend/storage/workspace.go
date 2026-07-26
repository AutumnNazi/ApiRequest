package storage

import (
	"database/sql"
	"time"

	"github.com/google/uuid"

	"apirequest/backend/model"
)

func nowMs() int64 { return time.Now().UnixMilli() }

// newId 生成 UUID v7（时间有序，ADR-006）
func newId() string {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.NewString() // 熵源异常时退回 v4，仅失去时序性
	}
	return id.String()
}

// EnsureDefaultWorkspace 返回第一个工作区；库为空时创建默认工作区
func (s *Store) EnsureDefaultWorkspace() (model.Workspace, error) {
	var w model.Workspace
	err := s.db.QueryRow(
		"SELECT id, name, type, created_at, updated_at FROM workspace ORDER BY created_at LIMIT 1",
	).Scan(&w.Id, &w.Name, &w.Type, &w.CreatedAt, &w.UpdatedAt)
	if err == nil {
		return w, nil
	}
	if err != sql.ErrNoRows {
		return w, err
	}
	w = model.Workspace{
		Id: newId(), Name: "My Workspace", Type: "local",
		CreatedAt: nowMs(), UpdatedAt: nowMs(),
	}
	_, err = s.db.Exec(
		"INSERT INTO workspace (id, name, type, created_at, updated_at) VALUES (?,?,?,?,?)",
		w.Id, w.Name, w.Type, w.CreatedAt, w.UpdatedAt,
	)
	return w, err
}

// ListWorkspaces 列出全部工作区
func (s *Store) ListWorkspaces() ([]model.Workspace, error) {
	rows, err := s.db.Query("SELECT id, name, type, created_at, updated_at FROM workspace ORDER BY created_at")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Workspace{}
	for rows.Next() {
		var w model.Workspace
		if err := rows.Scan(&w.Id, &w.Name, &w.Type, &w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// CreateWorkspace 新建工作区
func (s *Store) CreateWorkspace(name string) (model.Workspace, error) {
	w := model.Workspace{
		Id: newId(), Name: name, Type: "local",
		CreatedAt: nowMs(), UpdatedAt: nowMs(),
	}
	_, err := s.db.Exec(
		"INSERT INTO workspace (id, name, type, created_at, updated_at) VALUES (?,?,?,?,?)",
		w.Id, w.Name, w.Type, w.CreatedAt, w.UpdatedAt)
	return w, err
}

// RenameWorkspace 重命名
func (s *Store) RenameWorkspace(id, name string) error {
	_, err := s.db.Exec("UPDATE workspace SET name = ?, updated_at = ? WHERE id = ?", name, nowMs(), id)
	return err
}

// DeleteWorkspace 删除工作区及其全部数据（节点/环境/全局变量/历史）
func (s *Store) DeleteWorkspace(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		"DELETE FROM history WHERE workspace_id = ?",
		"DELETE FROM environment WHERE workspace_id = ?",
		"DELETE FROM global_var WHERE workspace_id = ?",
		// example 经 node 级联：先删本工作区节点下的 example，再删 node
		`DELETE FROM example WHERE node_id IN (SELECT id FROM node WHERE workspace_id = ?)`,
		"DELETE FROM node WHERE workspace_id = ?",
		"DELETE FROM workspace WHERE id = ?",
	} {
		if _, err := tx.Exec(q, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
