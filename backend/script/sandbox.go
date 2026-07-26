// Package script 提供 goja 沙箱与 Postman 兼容的 pm.* API 子集
// （docs/request-lifecycle.md §3；ADR-008）。
// 每次执行新建 Runtime，避免全局状态跨请求泄漏；超时用 vm.Interrupt 中断。
package script

import (
	"fmt"
	"time"

	"github.com/dop251/goja"

	"apirequest/backend/model"
)

// VarChanges 脚本对某作用域变量的变更缓冲（Go 统一提交，避免竞态）
type VarChanges struct {
	Set   map[string]string
	Unset map[string]bool
}

func newVarChanges() *VarChanges {
	return &VarChanges{Set: map[string]string{}, Unset: map[string]bool{}}
}

// Empty 是否无变更
func (c *VarChanges) Empty() bool { return len(c.Set) == 0 && len(c.Unset) == 0 }

// Result 一次脚本执行的产出
type Result struct {
	TestResults []model.TestResult
	Logs        []string
	// 各作用域的变更（binding 层决定持久化到哪）
	EnvChanges        *VarChanges
	CollectionChanges *VarChanges
	GlobalChanges     *VarChanges
	// 前置脚本对请求的修改
	MutatedRequest *model.HttpRequest
}

// Sandbox 一次请求内的脚本执行环境（前置 + 测试共享变量变更缓冲）
type Sandbox struct {
	timeout time.Duration

	// 合并只读视图（pm.variables.get 用），优先级已叠加
	merged map[string]string
	// 各作用域当前值（get 反映本作用域 + 脚本内变更）
	envVars, colVars, globalVars map[string]string

	envChanges, colChanges, globalChanges *VarChanges

	request  *model.HttpRequest
	response *model.ResponseResult

	testResults []model.TestResult
	logs        []string
	// 脚本执行结束后的回写钩子（pm.request 的 method/url 同步等）
	onFinish []func()
}

// NewSandbox 创建沙箱。各作用域传入当前生效值的副本。
func NewSandbox(timeout time.Duration, merged, envVars, colVars, globalVars map[string]string) *Sandbox {
	cp := func(m map[string]string) map[string]string {
		out := map[string]string{}
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	return &Sandbox{
		timeout:       timeout,
		merged:        cp(merged),
		envVars:       cp(envVars),
		colVars:       cp(colVars),
		globalVars:    cp(globalVars),
		envChanges:    newVarChanges(),
		colChanges:    newVarChanges(),
		globalChanges: newVarChanges(),
		testResults:   []model.TestResult{},
		logs:          []string{},
	}
}

// SetRequest 注入可变请求（前置脚本阶段）
func (s *Sandbox) SetRequest(req *model.HttpRequest) { s.request = req }

// SetResponse 注入只读响应（测试脚本阶段）
func (s *Sandbox) SetResponse(resp *model.ResponseResult) { s.response = resp }

// Run 执行一段脚本。phase 为 "pre" 或 "test"（错误归因用）。
func (s *Sandbox) Run(code, phase string) error {
	if code == "" {
		return nil
	}
	vm := goja.New()
	vm.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))

	// 沙箱约束：不注入 require/fs/fetch；仅暴露 pm 与 console
	if err := s.injectConsole(vm); err != nil {
		return err
	}
	if err := s.injectPM(vm); err != nil {
		return err
	}

	// 超时中断（wall-clock）
	timer := time.AfterFunc(s.timeout, func() {
		vm.Interrupt("script timeout")
	})
	defer timer.Stop()

	_, err := vm.RunString(code)
	for _, fn := range s.onFinish {
		fn()
	}
	s.onFinish = nil
	if err != nil {
		return scriptError(err, phase)
	}
	return nil
}

// Result 汇总执行产出
func (s *Sandbox) Result() Result {
	return Result{
		TestResults:       s.testResults,
		Logs:              s.logs,
		EnvChanges:        s.envChanges,
		CollectionChanges: s.colChanges,
		GlobalChanges:     s.globalChanges,
		MutatedRequest:    s.request,
	}
}

func scriptError(err error, phase string) *model.AppError {
	ae := &model.AppError{Kind: model.KindScript, Phase: phase, Detail: err.Error()}
	if ex, ok := err.(*goja.Exception); ok {
		ae.Detail = ex.Value().String()
	}
	if _, ok := err.(*goja.InterruptedError); ok {
		ae.Detail = "script timeout"
	}
	return ae
}

func (s *Sandbox) injectConsole(vm *goja.Runtime) error {
	logFn := func(level string) func(call goja.FunctionCall) goja.Value {
		return func(call goja.FunctionCall) goja.Value {
			parts := make([]string, len(call.Arguments))
			for i, a := range call.Arguments {
				parts[i] = stringify(a)
			}
			line := joinSpace(parts)
			if level != "log" {
				line = "[" + level + "] " + line
			}
			s.logs = append(s.logs, line)
			return goja.Undefined()
		}
	}
	console := vm.NewObject()
	console.Set("log", logFn("log"))
	console.Set("info", logFn("info"))
	console.Set("warn", logFn("warn"))
	console.Set("error", logFn("error"))
	return vm.Set("console", console)
}

func stringify(v goja.Value) string {
	if v == nil || goja.IsUndefined(v) {
		return "undefined"
	}
	if goja.IsNull(v) {
		return "null"
	}
	exported := v.Export()
	switch exported.(type) {
	case map[string]interface{}, []interface{}:
		return fmt.Sprintf("%v", exported)
	}
	return v.String()
}

func joinSpace(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " "
		}
		out += p
	}
	return out
}
