package script

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dop251/goja"

	"apirequest/backend/model"
)

// injectPM 注入 pm 对象（docs/request-lifecycle.md §3.2 的必做集）
func (s *Sandbox) injectPM(vm *goja.Runtime) error {
	pm := vm.NewObject()

	// pm.environment / pm.collectionVariables / pm.globals：各作用域读写
	pm.Set("environment", s.varScope(vm, s.envVars, s.envChanges))
	pm.Set("collectionVariables", s.varScope(vm, s.colVars, s.colChanges))
	pm.Set("globals", s.varScope(vm, s.globalVars, s.globalChanges))

	// pm.variables：合并只读视图 + replaceIn
	variables := vm.NewObject()
	variables.Set("get", func(name string) goja.Value {
		if v, ok := s.merged[name]; ok {
			return vm.ToValue(v)
		}
		return goja.Undefined()
	})
	variables.Set("has", func(name string) bool {
		_, ok := s.merged[name]
		return ok
	})
	variables.Set("replaceIn", func(input string) string {
		out := input
		for k, v := range s.merged {
			out = strings.ReplaceAll(out, "{{"+k+"}}", v)
		}
		return out
	})
	pm.Set("variables", variables)

	// pm.request：前置阶段可变
	if s.request != nil {
		if err := s.injectRequest(vm, pm); err != nil {
			return err
		}
	}
	// pm.response：测试阶段只读
	if s.response != nil {
		if err := s.injectResponse(vm, pm); err != nil {
			return err
		}
	}

	// pm.test(name, fn)：收集断言结果，fn 抛错 = 失败
	pm.Set("test", func(name string, fn goja.Callable) {
		_, err := fn(goja.Undefined())
		tr := model.TestResult{Name: name, Pass: err == nil}
		if err != nil {
			if ex, ok := err.(*goja.Exception); ok {
				tr.Error = ex.Value().String()
			} else {
				tr.Error = err.Error()
			}
		}
		s.testResults = append(s.testResults, tr)
	})

	// pm.expect：精简 chai BDD 子集
	if err := injectExpect(vm, pm); err != nil {
		return err
	}

	// pm.sendRequest(req, cb)：受控通道回调 Go 的 http 引擎（docs/request-lifecycle.md §3.2）。
	// goja 单线程执行，同步调用后立即回调 cb(err, response)。
	if s.SendFunc != nil {
		pm.Set("sendRequest", func(reqVal goja.Value, cb goja.Callable) {
			req, perr := parseScriptRequest(reqVal)
			if perr != nil {
				cb(goja.Undefined(), vm.ToValue(perr.Error()), goja.Undefined())
				return
			}
			res, serr := s.SendFunc(req)
			if serr != nil {
				cb(goja.Undefined(), vm.ToValue(serr.Error()), goja.Undefined())
				return
			}
			respObj := vm.NewObject()
			respObj.Set("code", res.Status)
			respObj.Set("status", res.StatusText)
			respObj.Set("text", func() string { return res.Body.Text })
			respObj.Set("json", func() (goja.Value, error) {
				var out interface{}
				if err := json.Unmarshal([]byte(res.Body.Text), &out); err != nil {
					return nil, fmt.Errorf("response is not valid JSON: %w", err)
				}
				return vm.ToValue(out), nil
			})
			cb(goja.Undefined(), goja.Null(), respObj)
		})
	}

	return vm.Set("pm", pm)
}

// parseScriptRequest 解析 pm.sendRequest 的入参：字符串 URL 或 {url,method,header,body} 对象
func parseScriptRequest(v goja.Value) (model.HttpRequest, error) {
	req := model.HttpRequest{
		Method:   "GET",
		Params:   []model.KV{},
		Headers:  []model.KV{},
		Body:     model.Body{Kind: "none"},
		Auth:     model.Auth{Type: "none"},
		Settings: model.DefaultSettings(),
	}
	exported := v.Export()
	switch t := exported.(type) {
	case string:
		req.Url = t
	case map[string]interface{}:
		if u, ok := t["url"].(string); ok {
			req.Url = u
		}
		if m, ok := t["method"].(string); ok && m != "" {
			req.Method = strings.ToUpper(m)
		}
		if h, ok := t["header"].(map[string]interface{}); ok {
			for k, val := range h {
				req.Headers = append(req.Headers, model.KV{
					Key: k, Value: fmt.Sprintf("%v", val), Enabled: true,
				})
			}
		}
		if b, ok := t["body"].(map[string]interface{}); ok {
			if mode, _ := b["mode"].(string); mode == "raw" {
				raw, _ := b["raw"].(string)
				req.Body = model.Body{Kind: "raw", Language: "json", Text: raw}
			}
		}
	default:
		return req, fmt.Errorf("pm.sendRequest: unsupported request argument")
	}
	if req.Url == "" {
		return req, fmt.Errorf("pm.sendRequest: url is required")
	}
	return req, nil
}

// varScope 构造一个作用域对象：get 读本作用域当前值，set/unset 记入变更缓冲
func (s *Sandbox) varScope(vm *goja.Runtime, current map[string]string, changes *VarChanges) *goja.Object {
	obj := vm.NewObject()
	obj.Set("get", func(name string) goja.Value {
		if v, ok := current[name]; ok {
			return vm.ToValue(v)
		}
		return goja.Undefined()
	})
	obj.Set("set", func(name string, value goja.Value) {
		str := stringify(value)
		current[name] = str
		s.merged[name] = str // 同请求内后续读取立即可见
		changes.Set[name] = str
		delete(changes.Unset, name)
	})
	obj.Set("unset", func(name string) {
		delete(current, name)
		changes.Unset[name] = true
		delete(changes.Set, name)
	})
	obj.Set("has", func(name string) bool {
		_, ok := current[name]
		return ok
	})
	return obj
}

// injectRequest 暴露 pm.request（method/url/headers 可改）
func (s *Sandbox) injectRequest(vm *goja.Runtime, pm *goja.Object) error {
	req := vm.NewObject()
	req.Set("method", s.request.Method)
	req.Set("url", s.request.Url)

	headers := vm.NewObject()
	headers.Set("add", func(h map[string]interface{}) {
		key, _ := h["key"].(string)
		val, _ := h["value"].(string)
		if key != "" {
			s.request.Headers = append(s.request.Headers, model.KV{Key: key, Value: val, Enabled: true})
		}
	})
	headers.Set("upsert", func(h map[string]interface{}) {
		key, _ := h["key"].(string)
		val, _ := h["value"].(string)
		if key == "" {
			return
		}
		for i := range s.request.Headers {
			if strings.EqualFold(s.request.Headers[i].Key, key) {
				s.request.Headers[i].Value = val
				s.request.Headers[i].Enabled = true
				return
			}
		}
		s.request.Headers = append(s.request.Headers, model.KV{Key: key, Value: val, Enabled: true})
	})
	headers.Set("remove", func(key string) {
		out := s.request.Headers[:0]
		for _, h := range s.request.Headers {
			if !strings.EqualFold(h.Key, key) {
				out = append(out, h)
			}
		}
		s.request.Headers = out
	})
	req.Set("headers", headers)

	// 回写 method/url：脚本结束后由 defer 读取（goja 对象属性 → Go）
	pm.Set("request", req)

	// 执行结束时同步 method/url 的修改
	s.onFinish = append(s.onFinish, func() {
		if v := req.Get("method"); v != nil && !goja.IsUndefined(v) {
			s.request.Method = v.String()
		}
		if v := req.Get("url"); v != nil && !goja.IsUndefined(v) {
			s.request.Url = v.String()
		}
	})
	return nil
}

// injectResponse 暴露 pm.response（只读）
func (s *Sandbox) injectResponse(vm *goja.Runtime, pm *goja.Object) error {
	resp := vm.NewObject()
	resp.Set("code", s.response.Status)
	resp.Set("status", s.response.StatusText)
	resp.Set("responseTime", s.response.Timing.TotalMs)

	headerList := make([]map[string]string, len(s.response.Headers))
	for i, h := range s.response.Headers {
		headerList[i] = map[string]string{"key": h.Key, "value": h.Value}
	}
	headers := vm.NewObject()
	headers.Set("get", func(key string) goja.Value {
		for _, h := range s.response.Headers {
			if strings.EqualFold(h.Key, key) {
				return vm.ToValue(h.Value)
			}
		}
		return goja.Undefined()
	})
	headers.Set("all", func() []map[string]string { return headerList })
	resp.Set("headers", headers)

	resp.Set("text", func() string { return s.response.Body.Text })
	resp.Set("json", func() (goja.Value, error) {
		var out interface{}
		if err := json.Unmarshal([]byte(s.response.Body.Text), &out); err != nil {
			return nil, fmt.Errorf("response is not valid JSON: %w", err)
		}
		return vm.ToValue(out), nil
	})

	pm.Set("response", resp)
	return nil
}
