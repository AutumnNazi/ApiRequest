package binding

import (
	"context"
	"fmt"
	"time"

	"apirequest/backend/model"
	"apirequest/backend/storage"
)

type workspaceOperationOwner interface {
	cancelWorkspace(context.Context, string) error
	resumeWorkspace(string)
}

// NodeApi 集合树 CRUD 域
type NodeApi struct {
	store     *storage.Store
	operation workspaceOperationOwner
}

// NewNodeApi 构造
func NewNodeApi(store *storage.Store, operation workspaceOperationOwner) *NodeApi {
	return &NodeApi{store: store, operation: operation}
}

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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	blocked := false
	resumeBlocked := func() {
		if blocked {
			a.operation.resumeWorkspace(id)
		}
	}
	if a.operation != nil {
		blocked = true
		if err := a.operation.cancelWorkspace(ctx, id); err != nil {
			resumeBlocked()
			return model.WrapError(model.KindStorage, fmt.Errorf("stop workspace operations: %w", err))
		}
	}
	if err := a.store.DeleteWorkspace(id); err != nil {
		resumeBlocked()
		return model.WrapError(model.KindStorage, err)
	}
	return nil
}

// ListNodes lists the lightweight collection-tree projection.
func (a *NodeApi) ListNodes(workspaceId string) ([]model.NodeSummary, error) {
	nodes, err := a.store.ListNodeSummaries(workspaceId)
	if err != nil {
		return nil, model.WrapError(model.KindStorage, err)
	}
	return nodes, nil
}

// GetNode loads one full editable node after checking workspace ownership.
func (a *NodeApi) GetNode(workspaceId, nodeId string) (model.Node, error) {
	node, err := a.store.GetNode(workspaceId, nodeId)
	if err != nil {
		return node, model.WrapError(model.KindStorage, err)
	}
	return node, nil
}

// RenameNode updates only the node name.
func (a *NodeApi) RenameNode(workspaceId, nodeId, name string) error {
	if workspaceId == "" || nodeId == "" || name == "" {
		return model.NewError(model.KindValidation, "workspaceId, nodeId, and name are required")
	}
	if err := a.store.RenameNode(workspaceId, nodeId, name); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	return nil
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
func (a *NodeApi) DeleteNode(workspaceId, nodeId string) error {
	if err := a.store.DeleteNode(workspaceId, nodeId); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	return nil
}

// MoveNode 移动节点
func (a *NodeApi) MoveNode(workspaceId, nodeId, newParentId string, sortOrder float64) error {
	if err := a.store.MoveNode(workspaceId, nodeId, newParentId, sortOrder); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	return nil
}

// MoveNodes 原子移动多个节点，任一失败则全部回滚。
func (a *NodeApi) MoveNodes(workspaceId string, moves []model.NodeMove) error {
	if err := a.store.MoveNodes(workspaceId, moves); err != nil {
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

// ListHistory 查询轻量历史摘要页。
func (a *HistoryApi) ListHistory(workspaceId string, q model.HistoryQuery) (model.HistoryPage, error) {
	page, err := a.store.ListHistory(workspaceId, q)
	if err != nil {
		return model.HistoryPage{}, model.WrapError(model.KindStorage, err)
	}
	return page, nil
}

// GetHistory 按需加载单条历史详情。
func (a *HistoryApi) GetHistory(workspaceId, id string) (model.HistoryDetail, error) {
	detail, err := a.store.GetHistory(workspaceId, id)
	if err != nil {
		return detail, model.WrapError(model.KindStorage, err)
	}
	return detail, nil
}

// ClearHistory 清空历史
func (a *HistoryApi) ClearHistory(workspaceId string) error {
	if err := a.store.ClearHistory(workspaceId); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	return nil
}
