package script

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/dop251/goja"

	"apirequest/backend/model"
)

// injectPM 注入 pm 对象（docs/request-lifecycle.md §3.2 的必做集）。
// phase 决定 pm.info.eventName（prerequest | test），各段 Run 独立注入。
func (s *Sandbox) injectPM(vm *goja.Runtime, phase string) error {
	pm := vm.NewObject()

	// pm.info：执行上下文（请求标识 + Runner 迭代信息）
	if err := s.injectInfo(vm, pm, phase); err != nil {
		return err
	}

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

	// pm.setNextRequest(name)：测试脚本指定下一个执行的请求（Runner 流转控制）。
	// 传 null/undefined/空串 = 清除，回到自然顺序。仅收集，不做任何跳转动作。
	pm.Set("setNextRequest", func(call goja.FunctionCall) goja.Value {
		name := ""
		if arg := call.Argument(0); !goja.IsUndefined(arg) && !goja.IsNull(arg) {
			name = arg.String()
		}
		if name == "" {
			s.nextRequest = nil
		} else {
			s.nextRequest = &name
		}
		return goja.Undefined()
	})

	// pm.test(name, fn)：收集断言结果，fn 抛错 = 失败
	// 回调必须经 AssertFunction 判空：漏传时 goja 会把 goja.Callable 参数置 nil，直接调用即 panic。
	pm.Set("test", func(call goja.FunctionCall) goja.Value {
		// 漏传 name 时 goja 的 String() 会给出字面量 "undefined"，
		// 报告里显示成一条名叫 undefined 的用例，不如显式标注缺名
		name := "(unnamed test)"
		if arg := call.Argument(0); !goja.IsUndefined(arg) && !goja.IsNull(arg) {
			name = arg.String()
		}
		fn, ok := goja.AssertFunction(call.Argument(1))
		if !ok {
			panic(vm.NewTypeError("pm.test(name, fn) requires a function argument"))
		}
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
		return goja.Undefined()
	})

	// pm.expect：精简 chai BDD 子集
	if err := injectExpect(vm, pm); err != nil {
		return err
	}

	// pm.sendRequest(req, cb)：受控通道回调 Go 的 http 引擎（docs/request-lifecycle.md §3.2）。
	// goja 单线程执行，同步调用后立即回调 cb(err, response)。
	// 回调经 AssertFunction 判空：漏传时 goja 会把 goja.Callable 参数置 nil，直接调用即 panic。
	if s.SendFunc != nil {
		pm.Set("sendRequest", func(call goja.FunctionCall) goja.Value {
			cb, ok := goja.AssertFunction(call.Argument(1))
			if !ok {
				panic(vm.NewTypeError("pm.sendRequest(req, cb) requires a callback function"))
			}
			fail := func(msg string) goja.Value {
				_, _ = cb(goja.Undefined(), vm.ToValue(msg), goja.Undefined())
				return goja.Undefined()
			}
			req, perr := parseScriptRequest(call.Argument(0))
			if perr != nil {
				return fail(perr.Error())
			}
			res, serr := s.SendFunc(req)
			if serr != nil {
				return fail(serr.Error())
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
			_, _ = cb(goja.Undefined(), goja.Null(), respObj)
			return goja.Undefined()
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

// injectInfo 注入 pm.info（执行上下文）。eventName 按脚本阶段取
// prerequest/test（Postman 事件名子集），其余字段原样透传（零值可见）。
func (s *Sandbox) injectInfo(vm *goja.Runtime, pm *goja.Object, phase string) error {
	info := vm.NewObject()
	eventName := "test"
	if phase == "pre" {
		eventName = "prerequest"
	}
	info.Set("eventName", eventName)
	info.Set("iteration", s.info.Iteration)
	info.Set("iterationCount", s.info.IterationCount)
	info.Set("requestName", s.info.RequestName)
	info.Set("requestId", s.info.RequestId)
	return pm.Set("info", info)
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
	resp.Set("responseSize", s.response.SizeBytes)

	// pm.response.cookies：数组视图 + get/has（Postman List 的常用面）
	cookies := vm.NewArray()
	for i, c := range s.response.Cookies {
		if err := cookies.Set(strconv.Itoa(i), map[string]interface{}{
			"name": c.Name, "value": c.Value, "domain": c.Domain, "path": c.Path,
			"expires": c.Expires, "httpOnly": c.HttpOnly,
		}); err != nil {
			return err
		}
	}
	cookies.Set("get", func(name string) goja.Value {
		for _, c := range s.response.Cookies {
			if c.Name == name {
				return vm.ToValue(c.Value)
			}
		}
		return goja.Undefined()
	})
	cookies.Set("has", func(name string) bool {
		for _, c := range s.response.Cookies {
			if c.Name == name {
				return true
			}
		}
		return false
	})
	resp.Set("cookies", cookies)

	// pm.response.to.be.*/to.have.*：Postman 响应断言子集（失败抛 Error，pm.test 捕获）
	if err := s.injectResponseAssertions(vm, resp); err != nil {
		return err
	}

	pm.Set("response", resp)
	return nil
}

// responseToJS 构造 pm.response.to：工厂函数，入参为响应对象（code/status/
// responseTime/responseSize/headers.get/text/json）。be.* 为即断言 getter，
// have.* 为方法。语义对齐 Postman SDK 的响应断言（ok=200、success=2xx、
// error≥400 等家族）。
const responseToJS = `(function (r) {
  function fail(m) { throw new Error(m); }
  function is(c) { return r.code === c; }
  function range(lo, hi) { return r.code >= lo && r.code < hi; }
  var be = {}, have = {}, to = {};
  function def(k, fn) {
    Object.defineProperty(be, k, { get: function () { fn(); } });
  }
  def('ok', function () { if (!is(200)) fail('expected response to be OK but got ' + r.code); });
  def('created', function () { if (!is(201)) fail('expected response to be Created (201) but got ' + r.code); });
  def('accepted', function () { if (!is(202)) fail('expected response to be Accepted (202) but got ' + r.code); });
  def('noContent', function () { if (!is(204)) fail('expected response to be No Content (204) but got ' + r.code); });
  def('info', function () { if (!range(100, 200)) fail('expected response to be 1xx info but got ' + r.code); });
  def('success', function () { if (!range(200, 300)) fail('expected response to be 2xx success but got ' + r.code); });
  def('redirection', function () { if (!range(300, 400)) fail('expected response to be 3xx redirection but got ' + r.code); });
  def('clientError', function () { if (!range(400, 500)) fail('expected response to be 4xx client error but got ' + r.code); });
  def('serverError', function () { if (!(r.code >= 500)) fail('expected response to be 5xx server error but got ' + r.code); });
  def('badRequest', function () { if (!is(400)) fail('expected response to be Bad Request (400) but got ' + r.code); });
  def('unauthorized', function () { if (!is(401)) fail('expected response to be Unauthorized (401) but got ' + r.code); });
  def('forbidden', function () { if (!is(403)) fail('expected response to be Forbidden (403) but got ' + r.code); });
  def('notFound', function () { if (!is(404)) fail('expected response to be Not Found (404) but got ' + r.code); });
  def('rateLimited', function () { if (!is(429)) fail('expected response to be Too Many Requests (429) but got ' + r.code); });
  def('error', function () { if (!(r.code >= 400)) fail('expected response to be 4xx/5xx error but got ' + r.code); });
  have.status = function (expected) {
    if (typeof expected === 'number') {
      if (r.code !== expected) fail('expected response to have status code ' + expected + ' but got ' + r.code);
      return;
    }
    var got = String(r.status || '').toLowerCase();
    if (String(expected).toLowerCase() !== got) {
      fail('expected response to have status "' + expected + '" but got "' + r.status + '"');
    }
  };
  have.statusCode = have.status;
  have.header = function (key, value) {
    var v = r.headers.get(key);
    if (v === undefined) fail('expected response to have header "' + key + '"');
    else if (arguments.length > 1 && v !== value) {
      fail('expected response header "' + key + '" to be "' + value + '" but got "' + v + '"');
    }
  };
  have.bodyContains = function (str) {
    if (String(r.text()).indexOf(String(str)) === -1) {
      fail('expected response body to contain "' + str + '"');
    }
  };
  have.jsonBody = function () {
    try { r.json(); } catch (e) { fail('expected response body to be valid JSON'); }
  };
  have.responseTimeBelow = function (ms) {
    if (!(r.responseTime < ms)) {
      fail('expected response time to be below ' + ms + 'ms but got ' + r.responseTime + 'ms');
    }
  };
  have.responseSizeBelow = function (bytes) {
    if (!(r.responseSize < bytes)) {
      fail('expected response size to be below ' + bytes + ' bytes but got ' + r.responseSize);
    }
  };
  to.be = be;
  to.have = have;
  return to;
})`

// injectResponseAssertions 把 responseToJS 工厂以响应对象调用，产物挂为 to。
func (s *Sandbox) injectResponseAssertions(vm *goja.Runtime, resp *goja.Object) error {
	fn, err := vm.RunString(responseToJS)
	if err != nil {
		return err
	}
	callable, ok := goja.AssertFunction(fn)
	if !ok {
		return fmt.Errorf("response assertions: factory is not callable")
	}
	to, err := callable(goja.Undefined(), resp)
	if err != nil {
		return err
	}
	return resp.Set("to", to)
}
