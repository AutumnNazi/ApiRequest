// Mock 响应脚本：每个 Example 可携带一段 JS（goja，纯 Go），按请求内容动态生成
// 响应（docs/advanced.md §1.3）。契约：
//   - 全局只读对象 mockRequest：{ method, path, query: Map<string,string[]>,
//     queryFirst(name), headers: object, body: string }
//   - respond({ status?, headers?, body?, delayMs? }) 生成响应；未调用则回退静态示例
//
// 每次请求新建 Runtime（无全局泄漏），2s 看门狗中断死循环。
package mock

import (
	"fmt"
	"strings"
	"time"

	"github.com/dop251/goja"

	"apirequest/backend/model"
)

const mockScriptTimeout = 2 * time.Second

// MockRequest 传给脚本的请求上下文
type MockRequest struct {
	Method  string
	Path    string
	Query   map[string][]string
	Headers map[string]string
	Body    string
}

// MockResponse 脚本产出的响应
type MockResponse struct {
	Status  int
	Headers map[string]string
	Body    string
	DelayMs int
}

// RunMockScript 执行 mock 脚本。called=false 表示脚本未调用 respond（回退静态示例）。
func RunMockScript(script string, req MockRequest) (resp MockResponse, called bool, err error) {
	rt := goja.New()
	timer := time.AfterFunc(mockScriptTimeout, func() { rt.Interrupt("mock script timeout") })
	defer timer.Stop()

	query := map[string][]string{}
	for k, vs := range req.Query {
		query[k] = append([]string(nil), vs...)
	}
	headers := map[string]string{}
	for k, v := range req.Headers {
		headers[k] = v
	}

	// no-op console：脚本可安全 console.log（不落日志流，仅避免 ReferenceError）
	rt.Set("console", map[string]any{
		"log":   func(...any) {},
		"warn":  func(...any) {},
		"error": func(...any) {},
	})
	rt.Set("mockRequest", map[string]any{
		"method": req.Method,
		"path":   req.Path,
		"query":  query,
		"queryFirst": func(name string) any {
			if vs, ok := query[name]; ok && len(vs) > 0 {
				return vs[0]
			}
			return nil
		},
		"headers": headers,
		"body":    req.Body,
	})
	rt.Set("respond", func(out map[string]any) {
		called = true
		resp = MockResponse{Status: 200, Headers: map[string]string{}}
		if v, ok := out["status"].(int64); ok {
			resp.Status = int(v)
		} else if v, ok := out["status"].(float64); ok {
			resp.Status = int(v)
		} else if v, ok := out["status"].(int); ok {
			resp.Status = v
		}
		if v, ok := out["body"].(string); ok {
			resp.Body = v
		}
		if v, ok := out["headers"].(map[string]any); ok {
			for k, hv := range v {
				if s, ok := hv.(string); ok {
					resp.Headers[k] = s
				}
			}
		}
		if v, ok := out["delayMs"].(int64); ok {
			resp.DelayMs = int(v)
		} else if v, ok := out["delayMs"].(float64); ok {
			resp.DelayMs = int(v)
		} else if v, ok := out["delayMs"].(int); ok {
			resp.DelayMs = v
		}
	})

	_, execErr := rt.RunString(script)
	if execErr != nil {
		return MockResponse{}, false, model.NewError(model.KindScript,
			"mock script error: "+exceptionText(execErr))
	}
	if !called {
		return MockResponse{}, false, nil
	}
	if resp.Status < 100 || resp.Status > 599 {
		return MockResponse{}, true, model.NewError(model.KindValidation,
			fmt.Sprintf("mock script status out of range: %d", resp.Status))
	}
	return resp, true, nil
}

func exceptionText(err error) string {
	msg := err.Error()
	msg = strings.ReplaceAll(msg, "\n", " ")
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	return msg
}
