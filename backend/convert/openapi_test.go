package convert

import "testing"


// TestOpenapiImportExamples：responses 的 example/examples 提取为 ImportedExample，
// 挂在请求占位 id 上（ImportCommit 落库时经映射换算）。Mock Server 以 Example
// 为数据源，导入即具备 Mock 能力（docs/interop.md）。
func TestOpenapiImportExamples(t *testing.T) {
	payload := `{
		"openapi": "3.0.0",
		"info": {"title": "Examples API", "version": "1.0"},
		"paths": {
			"/users": {
				"get": {
					"operationId": "listUsers",
					"responses": {
						"200": {
							"content": {
								"application/json": {
									"example": {"items": [1, 2, 3]},
									"examples": {
										"empty": {"summary": "空列表", "value": {"items": []}}
									}
								}
							}
						},
						"404": {
							"content": {
								"application/json": {
									"example": {"error": "not found"}
								}
							}
						}
					}
				},
				"post": {
					"operationId": "createUser",
					"responses": {
						"201": {
							"content": {
								"application/json": {
									"schema": {"type": "object"}
								}
							}
						}
					}
				}
			}
		}
	}`
	result, err := Import("openapi", payload)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(result.Examples) != 3 {
		t.Fatalf("examples = %d (%+v), want 3", len(result.Examples), result.Examples)
	}
	// GET 请求的三条：单示例 + 命名多示例 + 404
	var names []string
	for _, ex := range result.Examples {
		names = append(names, ex.Name)
	}
	wantNames := []string{"200 响应示例", "200 空列表", "404 响应示例"}
	for _, w := range wantNames {
		found := false
		for _, n := range names {
			if n == w {
				found = true
			}
		}
		if !found {
			t.Fatalf("examples missing %q, got %v", w, names)
		}
	}
	// 请求占位 id 对齐（第一条示例挂在 GET 请求节点上）
	var getRequest string
	for _, child := range result.Children {
		if child.Request != nil && child.Request.Method == "GET" {
			getRequest = child.Id
		}
	}
	if result.Examples[0].RequestId != getRequest {
		t.Fatalf("example requestId = %q, want GET node %q", result.Examples[0].RequestId, getRequest)
	}
	// POST 无 example 只有 schema：不合成，且 warnings 提示
	// （schema-only 的 warning 在 Import 层汇总）
}
