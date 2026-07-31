package binding

import (
	"time"

	"apirequest/backend/model"
	"apirequest/backend/script"
	"apirequest/backend/storage"
	"apirequest/backend/template"
)

// scriptTimeout 单段脚本的执行超时（docs/request-lifecycle.md §3.3）
const scriptTimeout = 5 * time.Second

// executionContext 一次发送收集到的完整上下文
type executionContext struct {
	scope *template.Scope // 合并作用域（优先级已叠加）

	envVars, colVars, globalVars map[string]string // 各作用域独立视图
	secretValues                 []string

	env       *model.Environment // 激活环境（nil = 无）
	ancestors []model.Node       // 请求节点 → 集合根（脚本继承用；请求未保存时为空）

	preScripts  []string // 执行顺序：根 → 叶 → 请求级
	testScripts []string
}

// collectContext 合并变量作用域并收集继承脚本链
// （优先级低→高：global → collection 链 → environment → overrides）
func collectContext(store *storage.Store, req model.HttpRequest, sendCtx model.SendContext) (*executionContext, error) {
	ec := &executionContext{
		scope:      template.NewScope(),
		envVars:    map[string]string{},
		colVars:    map[string]string{},
		globalVars: map[string]string{},
	}

	// 1. 全局变量
	globals, err := store.GetGlobalVariables(sendCtx.WorkspaceId)
	if err != nil {
		return nil, model.WrapError(model.KindStorage, err)
	}
	ec.scope.PushVariables(globals)
	for _, v := range globals {
		if v.Enabled {
			ec.globalVars[v.Key] = v.Value
		}
		if v.Type == "secret" && v.Value != "" {
			ec.secretValues = append(ec.secretValues, v.Value)
		}
	}

	// 2. 集合链变量 + 脚本继承（根在前）
	if sendCtx.RequestId != "" {
		chain, err := store.NodeAncestorsInWorkspace(sendCtx.RequestId, sendCtx.WorkspaceId)
		if err != nil {
			return nil, model.WrapError(model.KindStorage, err)
		}
		ec.ancestors = chain
		// NodeAncestors 返回叶在前；反向遍历使根先生效
		for i := len(chain) - 1; i >= 0; i-- {
			n := chain[i]
			ec.scope.PushVariables(n.Variables)
			for _, v := range n.Variables {
				if v.Enabled {
					ec.colVars[v.Key] = v.Value
				}
				if v.Type == "secret" && v.Value != "" {
					ec.secretValues = append(ec.secretValues, v.Value)
				}
			}
			if n.PreScript != "" {
				ec.preScripts = append(ec.preScripts, n.PreScript)
			}
			if n.TestScript != "" {
				ec.testScripts = append(ec.testScripts, n.TestScript)
			}
		}
	}

	// 3. 激活环境（显式指定优先，否则用工作区激活的）
	var env model.Environment
	var haveEnv bool
	if sendCtx.EnvironmentId != "" {
		env, err = store.GetEnvironment(sendCtx.EnvironmentId)
		if err != nil {
			return nil, model.WrapError(model.KindStorage, err)
		}
		haveEnv = true
		if env.WorkspaceId != sendCtx.WorkspaceId {
			return nil, model.NewError(model.KindValidation, "environment belongs to a different workspace")
		}
	} else {
		env, haveEnv, err = store.ActiveEnvironment(sendCtx.WorkspaceId)
		if err != nil {
			return nil, model.WrapError(model.KindStorage, err)
		}
	}
	if haveEnv {
		ec.env = &env
		ec.scope.PushVariables(env.Variables)
		for _, v := range env.Variables {
			if v.Enabled {
				ec.envVars[v.Key] = v.Value
			}
			if v.Type == "secret" && v.Value != "" {
				ec.secretValues = append(ec.secretValues, v.Value)
			}
		}
	}

	// 4. 前端传入的本地覆盖（最高优先级）
	if len(sendCtx.VariableOverrides) > 0 {
		ec.scope.PushMap(sendCtx.VariableOverrides)
	}

	// 请求级脚本排在链尾（最后执行，最贴近请求）
	if req.PreScript != "" {
		ec.preScripts = append(ec.preScripts, req.PreScript)
	}
	if req.TestScript != "" {
		ec.testScripts = append(ec.testScripts, req.TestScript)
	}
	return ec, nil
}

// persistVariableChanges 把脚本的变量变更统一提交（docs/request-lifecycle.md §3.1）
func persistVariableChanges(store *storage.Store, ec *executionContext, workspaceId string, r script.Result) error {
	// 环境变更 → 激活环境（无激活环境时丢弃，与 Postman 行为一致）
	if ec.env != nil && !r.EnvChanges.Empty() {
		ec.env.Variables = applyChanges(ec.env.Variables, r.EnvChanges)
		if _, err := store.UpsertEnvironment(*ec.env); err != nil {
			return err
		}
	}
	// 集合变更 → 集合根节点
	if !r.CollectionChanges.Empty() && len(ec.ancestors) > 0 {
		root := ec.ancestors[len(ec.ancestors)-1]
		root.Variables = applyChanges(root.Variables, r.CollectionChanges)
		if _, err := store.UpsertNode(root); err != nil {
			return err
		}
	}
	// 全局变更
	if !r.GlobalChanges.Empty() {
		globals, err := store.GetGlobalVariables(workspaceId)
		if err != nil {
			return err
		}
		if err := store.SetGlobalVariables(workspaceId, applyChanges(globals, r.GlobalChanges)); err != nil {
			return err
		}
	}
	return nil
}

// applyChanges 把变更缓冲应用到变量列表（保序；新变量追加）
func applyChanges(vars []model.Variable, c *script.VarChanges) []model.Variable {
	out := make([]model.Variable, 0, len(vars))
	seen := map[string]bool{}
	for _, v := range vars {
		if c.Unset[v.Key] {
			continue
		}
		if nv, ok := c.Set[v.Key]; ok {
			v.Value = nv
			v.Enabled = true
			seen[v.Key] = true
		}
		out = append(out, v)
	}
	for k, v := range c.Set {
		if !seen[k] {
			out = append(out, model.Variable{Key: k, Value: v, Type: "default", Enabled: true})
		}
	}
	return out
}

// resolveInheritedAuth 请求 auth 为 inherit/空时，沿祖先链（叶→根）找最近的显式 auth
func resolveInheritedAuth(req *model.HttpRequest, ancestors []model.Node) {
	if req.Auth.Type != "" && req.Auth.Type != "inherit" {
		return
	}
	for _, n := range ancestors {
		if n.Auth != nil && n.Auth.Type != "" && n.Auth.Type != "inherit" && n.Auth.Type != "none" {
			req.Auth = *n.Auth
			return
		}
	}
	req.Auth = model.Auth{Type: "none"}
}
