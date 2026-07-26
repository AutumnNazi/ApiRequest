// Package template 实现 {{var}} 变量解析与模板渲染（docs/request-lifecycle.md §2）。
// 纯函数层，脱离 UI/存储可单测。
package template

import (
	"strings"

	"apirequest/backend/model"
)

// maxDepth 嵌套解析最大深度（值内含 {{x}} 时继续解析），防循环引用
const maxDepth = 5

// Scope 变量作用域链。按优先级从低到高叠加（后写覆盖先写）。
type Scope struct {
	vars map[string]string
}

// NewScope 创建空作用域
func NewScope() *Scope {
	return &Scope{vars: map[string]string{}}
}

// PushVariables 叠加一层变量（仅启用项生效）
func (s *Scope) PushVariables(vars []model.Variable) *Scope {
	for _, v := range vars {
		if v.Enabled && v.Key != "" {
			s.vars[v.Key] = v.Value
		}
	}
	return s
}

// PushMap 叠加一层 map 形式的覆盖（本地变量/脚本 set）
func (s *Scope) PushMap(m map[string]string) *Scope {
	for k, v := range m {
		s.vars[k] = v
	}
	return s
}

// Get 查变量
func (s *Scope) Get(name string) (string, bool) {
	v, ok := s.vars[name]
	return v, ok
}

// Set 写变量（脚本 pm.*.set 桥接用）
func (s *Scope) Set(name, value string) { s.vars[name] = value }

// Unset 删变量
func (s *Scope) Unset(name string) { delete(s.vars, name) }

// Snapshot 导出当前合并视图（脚本注入用）
func (s *Scope) Snapshot() map[string]string {
	out := make(map[string]string, len(s.vars))
	for k, v := range s.vars {
		out[k] = v
	}
	return out
}

// Resolve 解析字符串中的 {{var}} 与 {{$dynamic}}。未定义变量保留原样。
func Resolve(input string, scope *Scope) string {
	return resolveDepth(input, scope, 0)
}

func resolveDepth(input string, scope *Scope, depth int) string {
	if depth >= maxDepth || !strings.Contains(input, "{{") {
		return input
	}
	var b strings.Builder
	b.Grow(len(input))
	i := 0
	for i < len(input) {
		open := strings.Index(input[i:], "{{")
		if open < 0 {
			b.WriteString(input[i:])
			break
		}
		open += i
		close := strings.Index(input[open+2:], "}}")
		if close < 0 {
			b.WriteString(input[i:])
			break
		}
		close += open + 2

		b.WriteString(input[i:open])
		name := strings.TrimSpace(input[open+2 : close])

		if val, ok := lookup(name, scope); ok {
			// 值本身可能还含 {{x}}：递归一层
			b.WriteString(resolveDepth(val, scope, depth+1))
		} else {
			// 未定义：原样保留
			b.WriteString(input[open : close+2])
		}
		i = close + 2
	}
	return b.String()
}

func lookup(name string, scope *Scope) (string, bool) {
	if strings.HasPrefix(name, "$") {
		return dynamicVar(name)
	}
	return scope.Get(name)
}

// ResolveRequest 对请求全字段做变量替换：URL、query、header、body 文本、auth 参数。
// binary body 的文件路径仅路径本身参与替换（docs/request-lifecycle.md §2.3）。
func ResolveRequest(req model.HttpRequest, scope *Scope) model.HttpRequest {
	out := req
	out.Url = Resolve(req.Url, scope)

	out.Params = resolveKVs(req.Params, scope)
	out.Headers = resolveKVs(req.Headers, scope)

	b := req.Body
	switch b.Kind {
	case "raw", "graphql":
		b.Text = Resolve(b.Text, scope)
		b.Query = Resolve(b.Query, scope)
		b.Variables = Resolve(b.Variables, scope)
	case "urlencoded", "formdata":
		items := make([]model.FormItem, len(b.Items))
		for i, it := range b.Items {
			it.Key = Resolve(it.Key, scope)
			it.Value = Resolve(it.Value, scope)
			it.Path = Resolve(it.Path, scope)
			items[i] = it
		}
		b.Items = items
	case "binary":
		b.Path = Resolve(b.Path, scope)
	}
	out.Body = b

	if req.Auth.Params != nil {
		params := make(map[string]string, len(req.Auth.Params))
		for k, v := range req.Auth.Params {
			params[k] = Resolve(v, scope)
		}
		out.Auth.Params = params
	}
	return out
}

func resolveKVs(kvs []model.KV, scope *Scope) []model.KV {
	out := make([]model.KV, len(kvs))
	for i, kv := range kvs {
		kv.Key = Resolve(kv.Key, scope)
		kv.Value = Resolve(kv.Value, scope)
		out[i] = kv
	}
	return out
}
