package binding

import (
	"apirequest/backend/model"
	"apirequest/backend/storage"
)

// NodeApi 集合树 CRUD 域
type NodeApi struct {
	store *storage.Store
}

// NewNodeApi 构造
func NewNodeApi(store *storage.Store) *NodeApi { return &NodeApi{store: store} }

// GetDefaultWorkspace 返回默认工作区（无则创建）
func (a *NodeApi) GetDefaultWorkspace() (model.Workspace, error) {
	w, err := a.store.EnsureDefaultWorkspace()
	if err != nil {
		return w, model.WrapError(model.KindStorage, err)
	}
	return w, nil
}

// ListWorkspaces 列出全部工作区
func (a *NodeApi) ListWorkspaces() ([]model.Workspace, error) {
	out, err := a.store.ListWorkspaces()
	if err != nil {
		return nil, model.WrapError(model.KindStorage, err)
	}
	return out, nil
}

// CreateWorkspace 新建工作区
func (a *NodeApi) CreateWorkspace(name string) (model.Workspace, error) {
	if name == "" {
		return model.Workspace{}, model.NewError(model.KindValidation, "workspace name is required")
	}
	w, err := a.store.CreateWorkspace(name)
	if err != nil {
		return w, model.WrapError(model.KindStorage, err)
	}
	return w, nil
}

// RenameWorkspace 重命名工作区
func (a *NodeApi) RenameWorkspace(id, name string) error {
	if name == "" {
		return model.NewError(model.KindValidation, "workspace name is required")
	}
	if err := a.store.RenameWorkspace(id, name); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	return nil
}

// DeleteWorkspace 删除工作区及全部数据；最后一个工作区不可删
func (a *NodeApi) DeleteWorkspace(id string) error {
	all, err := a.store.ListWorkspaces()
	if err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	if len(all) <= 1 {
		return model.NewError(model.KindValidation, "cannot delete the last workspace")
	}
	if err := a.store.DeleteWorkspace(id); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	return nil
}

// ListNodes 列出工作区全部节点
func (a *NodeApi) ListNodes(workspaceId string) ([]model.Node, error) {
	nodes, err := a.store.ListNodes(workspaceId)
	if err != nil {
		return nil, model.WrapError(model.KindStorage, err)
	}
	return nodes, nil
}

// UpsertNode 新增或更新节点
func (a *NodeApi) UpsertNode(n model.Node) (model.Node, error) {
	if n.WorkspaceId == "" {
		return n, model.NewError(model.KindValidation, "workspaceId is required")
	}
	if n.Kind != "collection" && n.Kind != "folder" && n.Kind != "request" {
		return n, model.NewError(model.KindValidation, "invalid node kind: "+n.Kind)
	}
	out, err := a.store.UpsertNode(n)
	if err != nil {
		return out, model.WrapError(model.KindStorage, err)
	}
	return out, nil
}

// DeleteNode 软删除节点及后代
func (a *NodeApi) DeleteNode(nodeId string) error {
	if err := a.store.DeleteNode(nodeId); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	return nil
}

// MoveNode 移动节点
func (a *NodeApi) MoveNode(nodeId, newParentId string, sortOrder float64) error {
	if err := a.store.MoveNode(nodeId, newParentId, sortOrder); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	return nil
}

// HistoryApi 历史域
type HistoryApi struct {
	store *storage.Store
}

// NewHistoryApi 构造
func NewHistoryApi(store *storage.Store) *HistoryApi { return &HistoryApi{store: store} }

// ListHistory 查询历史
func (a *HistoryApi) ListHistory(workspaceId string, q model.HistoryQuery) ([]model.HistoryItem, error) {
	items, err := a.store.ListHistory(workspaceId, q)
	if err != nil {
		return nil, model.WrapError(model.KindStorage, err)
	}
	return items, nil
}

// ClearHistory 清空历史
func (a *HistoryApi) ClearHistory(workspaceId string) error {
	if err := a.store.ClearHistory(workspaceId); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	return nil
}
