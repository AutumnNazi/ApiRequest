package binding

import (
	"apirequest/backend/codegen"
	"apirequest/backend/convert"
	"apirequest/backend/mirror"
	"apirequest/backend/model"
	"apirequest/backend/secrets"
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

// ImportCommit 确认导入：把预览树落库（id 重新生成，占位 id 映射到新 id）。
// 导入器建议的环境（OpenAPI servers 等）一并创建（不激活），返回创建的环境列表。
func (a *ConvertApi) ImportCommit(workspaceId string, res convert.ImportResult) (convert.ImportCommitResult, error) {
	if workspaceId == "" {
		return convert.ImportCommitResult{}, model.NewError(model.KindValidation, "workspaceId is required")
	}
	saved, err := a.store.ImportNodeTree(workspaceId, res.Collection, res.Children)
	if err != nil {
		return convert.ImportCommitResult{}, model.WrapError(model.KindStorage, err)
	}
	out := convert.ImportCommitResult{Collection: saved}
	for _, sug := range res.SuggestedEnvironments {
		env, envErr := a.store.UpsertEnvironment(model.Environment{
			WorkspaceId: workspaceId,
			Name:        sug.Name,
			Variables:   sug.Variables,
		})
		if envErr != nil {
			return out, model.WrapError(model.KindStorage, envErr)
		}
		out.Environments = append(out.Environments, env)
	}
	return out, nil
}

// ExportData 导出集合为目标格式文本
func (a *ConvertApi) ExportData(collectionId, format string) (string, error) {
	nodes, collection, err := a.collectTree(collectionId)
	if err != nil {
		return "", err
	}
	collection, nodes = redactExportTree(collection, nodes)
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

// ExportMirror 把集合导出为 Git 友好目录镜像（docs/decisions.md OPEN-004）
func (a *ConvertApi) ExportMirror(collectionId, dir string) error {
	if dir == "" {
		return model.NewError(model.KindValidation, "target directory is required")
	}
	nodes, collection, err := a.collectTree(collectionId)
	if err != nil {
		return err
	}
	collection, nodes = redactExportTree(collection, nodes)
	if err := mirror.Export(dir, collection, nodes); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	return nil
}

func redactExportTree(collection model.Node, nodes []model.Node) (model.Node, []model.Node) {
	collection = secrets.RedactNode(collection)
	redacted := make([]model.Node, len(nodes))
	for i, node := range nodes {
		redacted[i] = secrets.RedactNode(node)
	}
	return collection, redacted
}

// ImportMirror 从镜像目录导入为新集合（预览语义同 ImportPreview→ImportCommit 的合并版：
// 镜像导入低频且来源可信度高，直接落库）
func (a *ConvertApi) ImportMirror(workspaceId, dir string) (model.Node, error) {
	if workspaceId == "" || dir == "" {
		return model.Node{}, model.NewError(model.KindValidation, "workspaceId and dir are required")
	}
	collection, children, err := mirror.Import(dir)
	if err != nil {
		return model.Node{}, model.WrapError(model.KindImport, err)
	}
	out, err := a.ImportCommit(workspaceId, convert.ImportResult{Collection: collection, Children: children})
	return out.Collection, err
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
