package binding

import (
	"apirequest/backend/model"
	"apirequest/backend/storage"
)

// EnvApi 环境与全局变量域（docs/api-contract.md §4）
type EnvApi struct {
	store *storage.Store
}

// NewEnvApi 构造
func NewEnvApi(store *storage.Store) *EnvApi { return &EnvApi{store: store} }

// ListEnvironments 列出环境
func (a *EnvApi) ListEnvironments(workspaceId string) ([]model.Environment, error) {
	out, err := a.store.ListEnvironments(workspaceId)
	if err != nil {
		return nil, model.WrapError(model.KindStorage, err)
	}
	return out, nil
}

// UpsertEnvironment 新增或更新环境
func (a *EnvApi) UpsertEnvironment(e model.Environment) (model.Environment, error) {
	if e.WorkspaceId == "" {
		return e, model.NewError(model.KindValidation, "workspaceId is required")
	}
	if e.Name == "" {
		return e, model.NewError(model.KindValidation, "environment name is required")
	}
	out, err := a.store.UpsertEnvironment(e)
	if err != nil {
		return out, model.WrapError(model.KindStorage, err)
	}
	return out, nil
}

// DeleteEnvironment 删除环境
func (a *EnvApi) DeleteEnvironment(envId string) error {
	if err := a.store.DeleteEnvironment(envId); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	return nil
}

// SetActiveEnvironment 激活环境（envId 空 = No Environment）
func (a *EnvApi) SetActiveEnvironment(workspaceId, envId string) error {
	if err := a.store.SetActiveEnvironment(workspaceId, envId); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	return nil
}

// GetGlobalVariables 读全局变量
func (a *EnvApi) GetGlobalVariables(workspaceId string) ([]model.Variable, error) {
	out, err := a.store.GetGlobalVariables(workspaceId)
	if err != nil {
		return nil, model.WrapError(model.KindStorage, err)
	}
	return out, nil
}

// SetGlobalVariables 写全局变量
func (a *EnvApi) SetGlobalVariables(workspaceId string, vars []model.Variable) error {
	if err := a.store.SetGlobalVariables(workspaceId, vars); err != nil {
		return model.WrapError(model.KindStorage, err)
	}
	return nil
}
