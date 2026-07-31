package model

import "testing"

// 防 Body.Kind / Lang / FormItem.Type / Auth.Type 枚举漂移回归。
// 这些常量是前后端契约，TS 端需对应更新（见 docs/data-model.md §3）。
func TestEnumContractStable(t *testing.T) {
	// Body.Kind
	for _, v := range []string{BodyNone, BodyRaw, BodyFormData, BodyUrlEncoded, BodyBinary, BodyGraphQL} {
		if v == "" {
			t.Errorf("Body.Kind 常量不能为空")
		}
	}
	// Language
	for _, v := range []string{LangJSON, LangXML, LangHTML, LangText} {
		if v == "" {
			t.Errorf("Language 常量不能为空")
		}
	}
	// FormItem.Type
	for _, v := range []string{FormText, FormFile} {
		if v == "" {
			t.Errorf("FormItem.Type 常量不能为空")
		}
	}
	// Auth.Type 不查表分支
	for _, v := range []string{AuthNone, AuthInherit} {
		if v == "" {
			t.Errorf("Auth.Type 常量不能为空")
		}
	}
	// 防回归：枚举值拼写不能漂移（这是文档/TS 契约的实际取值）
	wantBodies := map[string]string{
		BodyNone:       "none",
		BodyRaw:        "raw",
		BodyFormData:   "formdata",
		BodyUrlEncoded: "urlencoded",
		BodyBinary:     "binary",
		BodyGraphQL:    "graphql",
	}
	for k, w := range wantBodies {
		if k != w {
			t.Errorf("Body.Kind 漂移: got %q, want %q", k, w)
		}
	}
}
