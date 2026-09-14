package requesturl

import (
	"net/url"
	"testing"

	"apirequest/backend/model"
)

func TestSetParamReplacesEveryDecodedKeyAndPreservesFragment(t *testing.T) {
	got := SetParam("https://x.io/items?api_key=old&tag=one&api%5Fkey=older#result", "api_key", "new key", true)
	want := "https://x.io/items?tag=one&api_key=new+key#result"
	if got != want {
		t.Fatalf("SetParam() = %q, want %q", got, want)
	}
}

// ── AddParams：执行链路（httpengine）的 query 合并 ──

func TestAddParamsSkipsDisabledAndEmptyKeys(t *testing.T) {
	query := url.Values{}
	AddParams(query, []model.KV{
		{Key: "keep", Value: "1", Enabled: true},
		{Key: "skipped", Value: "2", Enabled: false},
		{Key: "", Value: "3", Enabled: true},
	})
	if got := query.Encode(); got != "keep=1" {
		t.Fatalf("AddParams() = %q, want only the enabled non-empty key", got)
	}
}

// URL 里已有的 pair 不重复追加：url.Values 的 key 已解码，
// 因此 percent-encoded 的同名 key 也算命中（与 SetParam 的判定一致）
func TestAddParamsSkipsPairsAlreadyInQuery(t *testing.T) {
	query := url.Values{"api key": []string{"v 1"}}
	AddParams(query, []model.KV{{Key: "api key", Value: "v 1", Enabled: true}})
	if values := query["api key"]; len(values) != 1 {
		t.Fatalf("query[%q] = %v, want the existing pair kept once", "api key", values)
	}
}

// 同 key 不同 value 是合法多值 query（Postman 语义），不能被去重吃掉
func TestAddParamsKeepsDistinctValuesForSameKey(t *testing.T) {
	query := url.Values{"tag": []string{"one"}}
	AddParams(query, []model.KV{
		{Key: "tag", Value: "two", Enabled: true},
		{Key: "tag", Value: "one", Enabled: true}, // 与 URL 中重复 → 跳过
		{Key: "tag", Value: "two", Enabled: true}, // 与本批次重复 → 跳过
	})
	if got := query["tag"]; len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Fatalf("query[tag] = %v, want [one two]", got)
	}
}

// ── AppendParams：保留调用方原始 URL 文本，只追加缺失的 pair ──

func TestAppendParams(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		params []model.KV
		encode bool
		want   string
	}{
		{
			name:   "编码模式转义 key 与 value（codegen 需要合法 URL 字面）",
			raw:    "https://x.io/items",
			params: []model.KV{{Key: "api key", Value: "a b&c", Enabled: true}},
			encode: true,
			want:   "https://x.io/items?api+key=a+b%26c",
		},
		{
			name:   "非编码模式原样保留模板占位符（导出给用户自行替换）",
			raw:    "https://x.io/items",
			params: []model.KV{{Key: "token", Value: "{{TOKEN}}", Enabled: true}},
			encode: false,
			want:   "https://x.io/items?token={{TOKEN}}",
		},
		{
			name:   "已有 query 用 & 续接，fragment 留在末尾",
			raw:    "https://x.io/items?tag=one#result",
			params: []model.KV{{Key: "page", Value: "2", Enabled: true}},
			encode: true,
			want:   "https://x.io/items?tag=one&page=2#result",
		},
		{
			name:   "URL 中已存在的 pair 不重复追加（percent-encoded 亦算命中）",
			raw:    "https://x.io/items?api%5Fkey=v",
			params: []model.KV{{Key: "api_key", Value: "v", Enabled: true}},
			encode: true,
			want:   "https://x.io/items?api%5Fkey=v",
		},
		{
			name:   "无可追加项时原样返回（fragment 不丢）",
			raw:    "https://x.io/items?tag=one#result",
			params: []model.KV{{Key: "tag", Value: "one", Enabled: true}, {Key: "off", Value: "x"}},
			encode: true,
			want:   "https://x.io/items?tag=one#result",
		},
		{
			name:   "模板 URL 无法解析也不破坏原文（{{base}} 保留）",
			raw:    "{{base}}/items",
			params: []model.KV{{Key: "page", Value: "2", Enabled: true}},
			encode: false,
			want:   "{{base}}/items?page=2",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AppendParams(tc.raw, tc.params, tc.encode); got != tc.want {
				t.Fatalf("AppendParams() = %q, want %q", got, tc.want)
			}
		})
	}
}

// ── SetParam：apikey-in-query 覆盖写入（每个同名 key 都被替换成单个值）──

func TestSetParam(t *testing.T) {
	cases := []struct {
		name  string
		raw   string
		key   string
		value string
		want  string
	}{
		{
			name: "空 key 原样返回（不产生空参数）",
			raw:  "https://x.io/items?tag=one", key: "", value: "v",
			want: "https://x.io/items?tag=one",
		},
		{
			name: "无 query 时创建 query",
			raw:  "https://x.io/items", key: "api_key", value: "secret",
			want: "https://x.io/items?api_key=secret",
		},
		{
			name: "保留无关项与 fragment",
			raw:  "https://x.io/items?tag=one#result", key: "api_key", value: "secret",
			want: "https://x.io/items?tag=one&api_key=secret#result",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SetParam(tc.raw, tc.key, tc.value, true); got != tc.want {
				t.Fatalf("SetParam() = %q, want %q", got, tc.want)
			}
		})
	}
}

// 非编码模式的 SetParam 保留模板占位符（cURL 导出链路）
func TestSetParamRawModeKeepsTemplatePlaceholder(t *testing.T) {
	got := SetParam("https://x.io/items?api_key=old", "api_key", "{{API_KEY}}", false)
	want := "https://x.io/items?api_key={{API_KEY}}"
	if got != want {
		t.Fatalf("SetParam() = %q, want %q", got, want)
	}
}
