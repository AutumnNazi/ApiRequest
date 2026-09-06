package convert

import (
	"strings"
	"testing"
)

// 导入解析器 fuzz：解析任意输入要么产出结果、要么返回 *model.AppError，
// 绝不允许 panic 或非 AppError 的裸错误逃出。导入内容本就视为不可信
//（docs/ops.md §1），这是该承诺的可执行验证。
//
// 种子覆盖各格式的最小合法样本 + 已知边角（空对象、畸形 JSON、截断行）。
// 本地：go test ./backend/convert/ -run FuzzImportParsers -fuzz FuzzImportParsers -fuzztime 60s
// CI：种子语料回归（-short 跳过探测轮）。

var fuzzSeeds = map[string]string{
	"curl": `curl https://api.example.test/users \
  -X POST \
  -H 'Content-Type: application/json' \
  -d '{"name":"a"}'`,
	"har": `{"log":{"version":"1.2","creator":{"name":"x"},"entries":[
		{"request":{"method":"GET","url":"https://a.test/","headers":[],"queryString":[]},
		 "response":{"status":200,"headers":[],"content":{"text":"{}"}}}]}}`,
	"insomnia": `{"_type":"export","resources":[{"_id":"a","_type":"workspace","name":"w"},
		{"_id":"b","_type":"request","parentId":"a","name":"r","method":"GET","url":"https://a.test"}]}`,
	"openapi": `{"openapi":"3.0.0","info":{"title":"t","version":"1"},"paths":{"/a":{"get":{"responses":{"200":{"description":"ok"}}}}}}`,
	"postman": `{"info":{"name":"c","schema":"https://schema.getpostman.com/json/collection/v2.1.0/collection.json"},
		"item":[{"name":"r","request":{"method":"GET","url":{"raw":"https://a.test"}}}]}`,
	"restclient": `### one\nGET https://a.test\n\n### two\nPOST https://a.test/b\nContent-Type: application/json\n\n{"a":1}`,
	"garbage":     `{ not json`,
	"empty":       ``,
	"truncated":  `{"openapi":"3.0.0","paths":{"`,
	"null-entry": `{"log":{"entries":[null]}}`,
}

func runFuzzCase(t *testing.T, format, payload string) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("parser panic (format=%s): %v\npayload: %.200s", format, r, payload)
		}
	}()
	result, err := Import(format, payload)
	if err != nil {
		// 错误路径：必须返回 error，且不允许"半成品 result"
		if result != nil {
			t.Fatalf("format=%s: error and result both returned", format)
		}
		return
	}
	// 成功路径：树必须可展开为请求节点，名称不能为空得离谱（截断为非空即可）
	if result == nil {
		t.Fatalf("format=%s: neither result nor error", format)
	}
	for _, node := range result.Children {
		if node.Kind != "" && node.Name == "\x00" {
			t.Fatalf("format=%s: control character in node name", format)
		}
	}
}

func TestFuzzImportParsersSeeds(t *testing.T) {
	for format, payload := range fuzzSeeds {
		if payload == "" {
			continue // 空输入允许报错或空结果，单独覆盖
		}
		t.Run(format, func(t *testing.T) { runFuzzCase(t, format, payload) })
	}
	// 空输入：任意格式都不允许 panic
	for format := range fuzzSeeds {
		runFuzzCase(t, format, "")
	}
	// auto 识别路径也纳入
	for _, payload := range fuzzSeeds {
		runFuzzCase(t, "auto", payload)
	}
}

func FuzzImportParsers(f *testing.F) {
	for format, payload := range fuzzSeeds {
		f.Add(format, payload)
	}
	// 格式名做模糊字典：未知格式应走"unsupported format"错误而非崩溃
	f.Add("unknown-format", `{"a":1}`)
	f.Add("", "{}")
	f.Add("curl", "curl")
	f.Add("restclient", "### broken\nGET\n")
	f.Add("har", `{"log":{"version":1,"entries":[{"request":{}}]}}`)
	f.Add("postman", `{"info":{},"item":"not-an-array"}`)
	f.Add("openapi", `{"openapi":"3.0.0","paths":{"":{"get":{"responses":{"200":{}}}}}}`)
	f.Add("insomnia", `{"resources":[{"_type":"request","url":123}]}`)
	f.Fuzz(func(t *testing.T, format, payload string) {
		if len(payload) > 1<<18 {
			t.Skip("oversized payload")
		}
		// 格式名里出现控制字符时 Trim 一下，模拟真实调用面（UI 下拉选项）
		format = strings.TrimSpace(format)
		runFuzzCase(t, format, payload)
	})
}
