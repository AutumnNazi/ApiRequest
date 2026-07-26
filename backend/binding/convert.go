package binding

import (
	"apirequest/backend/codegen"
	"apirequest/backend/convert"
	"apirequest/backend/model"
	"apirequest/backend/storage"
)

// ConvertApi 导入导出域（docs/api-contract.md §4）
type ConvertApi struct {
	store *storage.Store
}

// NewConvertApi 构造
func NewConvertApi(store *storage.Store) *ConvertApi { return &ConvertApi{store: store} }

// ImportPreview 解析 payload 返回预览树（不落库）
func (a *ConvertApi) ImportPreview(format, payload string) (*convert.ImportResult, error) {
	return convert.Import(format, payload)
}

// ImportCommit 确认导入：把预览树落库（id 重新生成，占位 id 映射到新 id）
func (a *ConvertApi) ImportCommit(workspaceId string, res convert.ImportResult) (model.Node, error) {
	if workspaceId == "" {
		return model.Node{}, model.NewError(model.KindValidation, "workspaceId is required")
	}
	idMap := map[string]string{}

	root := res.Collection
	root.Id = ""
	root.WorkspaceId = workspaceId
	root.ParentId = ""
	saved, err := a.store.UpsertNode(root)
	if err != nil {
		return model.Node{}, model.WrapError(model.KindStorage, err)
	}
	idMap[res.Collection.Id] = saved.Id

	// children 按原顺序落库；parentId 经映射表转换（父节点总在子之前出现）
	for _, n := range res.Children {
		oldId := n.Id
		n.Id = ""
		n.WorkspaceId = workspaceId
		if mapped, ok := idMap[n.ParentId]; ok {
			n.ParentId = mapped
		} else {
			n.ParentId = saved.Id // 兜底挂到根
		}
		created, err := a.store.UpsertNode(n)
		if err != nil {
			return model.Node{}, model.WrapError(model.KindStorage, err)
		}
		idMap[oldId] = created.Id
	}
	return saved, nil
}

// ExportData 导出集合为目标格式文本
func (a *ConvertApi) ExportData(collectionId, format string) (string, error) {
	nodes, collection, err := a.collectTree(collectionId)
	if err != nil {
		return "", err
	}
	out, err := convert.Export(format, collection, nodes)
	if err != nil {
		return "", model.WrapError(model.KindImport, err)
	}
	return out, nil
}

// CodegenTargets 返回代码生成目标列表
func (a *ConvertApi) CodegenTargets() []codegen.Target {
	return codegen.Targets()
}

// GenerateCode 为请求生成目标语言代码片段
func (a *ConvertApi) GenerateCode(target string, req model.HttpRequest) (string, error) {
	return codegen.Generate(target, req)
}

// collectTree 取集合根与其全部后代
func (a *ConvertApi) collectTree(collectionId string) ([]model.Node, model.Node, error) {
	var collection model.Node
	// 根节点经 NodeAncestors 取（链首即自身）
	chain, err := a.store.NodeAncestors(collectionId)
	if err != nil || len(chain) == 0 {
		return nil, collection, model.NewError(model.KindStorage, "collection not found: "+collectionId)
	}
	collection = chain[0]

	all, err := a.store.ListNodes(collection.WorkspaceId)
	if err != nil {
		return nil, collection, model.WrapError(model.KindStorage, err)
	}
	// BFS 收集后代
	descendants := []model.Node{}
	frontier := map[string]bool{collection.Id: true}
	for len(frontier) > 0 {
		next := map[string]bool{}
		for _, n := range all {
			if frontier[n.ParentId] {
				descendants = append(descendants, n)
				next[n.Id] = true
			}
		}
		frontier = next
	}
	return descendants, collection, nil
}
