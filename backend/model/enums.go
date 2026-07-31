package model

// Body.Kind 合法值常量集中地，避免拼写漂移。
// 写入文档：docs/data-model.md §3 契约面。
const (
	BodyNone       = "none"
	BodyRaw        = "raw"
	BodyFormData   = "formdata"
	BodyUrlEncoded = "urlencoded"
	BodyBinary     = "binary"
	BodyGraphQL    = "graphql"
)

// Raw.Language 合法值
const (
	LangJSON = "json"
	LangXML  = "xml"
	LangHTML = "html"
	LangText = "text"
)

// FormItem.Type 合法值
const (
	FormText = "text"
	FormFile = "file"
)

// Auth.Type 合法值中"不查表"的几类（auth.Get 直接返回 nil provider）
const (
	AuthNone    = "none"
	AuthInherit = "inherit"
	// "" 也视作无认证——保留为零值便于前端省略字段
)
