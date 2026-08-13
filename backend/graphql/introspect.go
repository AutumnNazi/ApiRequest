// Package graphql 实现 GraphQL schema 内省与补全输入（docs/protocols.md §5）。
// 内省走标准 introspection query，schema JSON 用于前端补全 + 类型浏览。
package graphql

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"apirequest/backend/model"
)

// IntrospectConfig 内省请求配置
type IntrospectConfig struct {
	Url       string            `json:"url"`     // GraphQL endpoint
	Headers   map[string]string `json:"headers"` // 额外 header（如 Authorization）
	TimeoutMs int               `json:"timeoutMs,omitempty"`
}

// Schema 标准内省返回的精简表示（仅取补全需要的部分字段）
type Schema struct {
	QueryType        *TypeName   `json:"queryType"`
	MutationType     *TypeName   `json:"mutationType,omitempty"`
	SubscriptionType *TypeName   `json:"subscriptionType,omitempty"`
	Types            []FullType  `json:"types"`
	Directives       []Directive `json:"directives,omitempty"`
}

// TypeName 内省里只给 name 的 type 引用
type TypeName struct {
	Name string `json:"name"`
}

// FullType 完整类型描述
type FullType struct {
	Kind          string       `json:"kind"` // OBJECT | SCALAR | ENUM | INPUT_OBJECT | INTERFACE | UNION
	Name          string       `json:"name"`
	Description   string       `json:"description,omitempty"`
	Fields        []Field      `json:"fields,omitempty"`      // OBJECT/INTERFACE
	InputFields   []InputField `json:"inputFields,omitempty"` // INPUT_OBJECT
	EnumValues    []EnumValue  `json:"enumValues,omitempty"`  // ENUM
	Interfaces    []TypeName   `json:"interfaces,omitempty"`
	PossibleTypes []TypeName   `json:"possibleTypes,omitempty"` // UNION/INTERFACE
}

type Field struct {
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Type        *TypeRef     `json:"type"`
	Args        []InputValue `json:"args,omitempty"`
}

type InputField struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Type        *TypeRef `json:"type"`
}

type InputValue struct {
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Type         *TypeRef `json:"type"`
	DefaultValue *string  `json:"defaultValue,omitempty"`
}

type EnumValue struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Directive 标注 directive
type Directive struct {
	Name        string       `json:"name"`
	Description string       `json:"description,omitempty"`
	Locations   []string     `json:"locations"`
	Args        []InputValue `json:"args,omitempty"`
}

// TypeRef 类型引用（附带 OfType 嵌套描述 NON_NULL/LIST）
type TypeRef struct {
	Kind   string   `json:"kind"`
	Name   string   `json:"name,omitempty"`
	OfType *TypeRef `json:"ofType,omitempty"`
}

// 标准 introspection query（最小可用集合）
const introspectionQuery = ` query IntrospectionQuery {
  __schema {
    queryType { name }
    mutationType { name }
    subscriptionType { name }
    types {
      kind name description
      fields(includeDeprecated: true) {
        name description
        args { name description type { kind name ofType { kind name ofType { kind name ofType { kind name ofType { kind name } } } } } defaultValue }
        type { kind name ofType { kind name ofType { kind name ofType { kind name ofType { kind name } } } } }
      }
      inputFields { name description type { kind name ofType { kind name ofType { kind name ofType { kind name ofType { kind name } } } } } }
      interfaces { name }
      enumValues { name description }
      possibleTypes { name }
    }
    directives { name description locations args { name description type { kind name ofType { kind name ofType { kind name ofType { kind name } } } } defaultValue } }
  }
}`

const maxIntrospectionResponseBytes = 16 << 20

// Result Introspect 返回：schema JSON 字符串（可直接交付前端）+ 解析后的结构
type Result struct {
	// SchemaJSON 原始 JSON（前端补全扩展用）
	SchemaJSON string `json:"schemaJson"`
	// 简化的 Queries/Mutations/Subscriptions 列表（前端展示用）
	Queries       []FieldSummary `json:"queries"`
	Mutations     []FieldSummary `json:"mutations"`
	Subscriptions []FieldSummary `json:"subscriptions,omitempty"`
}

// FieldSummary 操作摘要
type FieldSummary struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Args JSON（如 [{"name":"id","type":"ID!"}]）便于前端简单渲染
	Args string `json:"args,omitempty"`
	// ReturnType 返回类型字符串（"User!" / "[User!]!" 等）
	ReturnType string `json:"returnType"`
	// ReturnKind is the unwrapped named kind (SCALAR/ENUM/OBJECT/etc.).
	ReturnKind string `json:"returnKind"`
}

// Introspect 让后端发起内省请求并把 schema 整理为补全输入。
// 不与 httpengine 复用：内省是只读动作，不走集合/历史/重定向等流程；
// 在这里发起独立的 HTTP POST（application/json）。
func Introspect(ctx context.Context, cfg IntrospectConfig) (*Result, error) {
	return IntrospectWithClient(ctx, cfg, http.DefaultClient)
}

// IntrospectWithClient executes introspection through the application's shared
// network policy client.
func IntrospectWithClient(ctx context.Context, cfg IntrospectConfig, client *http.Client) (*Result, error) {
	if strings.TrimSpace(cfg.Url) == "" {
		return nil, model.NewError(model.KindValidation, "url is required")
	}
	to := time.Duration(cfg.TimeoutMs) * time.Millisecond
	if to <= 0 {
		to = 20 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()

	body := fmt.Sprintf(`{"query":%s}`, jsonQuote(introspectionQuery))
	req, err := http.NewRequestWithContext(cctx, "POST", cfg.Url, bytes.NewReader([]byte(body)))
	if err != nil {
		return nil, model.WrapError(model.KindNetwork, err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range cfg.Headers {
		req.Header.Set(k, v)
	}

	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, model.WrapError(model.KindNetwork, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxIntrospectionResponseBytes+1))
	if err != nil {
		return nil, model.WrapError(model.KindNetwork, err)
	}
	if len(raw) > maxIntrospectionResponseBytes {
		return nil, model.NewError(model.KindNetwork,
			fmt.Sprintf("introspection response too large (max %d MiB)", maxIntrospectionResponseBytes>>20))
	}
	if resp.StatusCode >= 400 {
		return nil, model.NewError(model.KindNetwork,
			fmt.Sprintf("introspection HTTP %d: %s", resp.StatusCode, truncate(string(raw), 500)))
	}

	// 解析 GraphQL 标准响应壳：{ data: { __schema: {...} }, errors: [...] }
	var shell struct {
		Data struct {
			Schema Schema `json:"__schema"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &shell); err != nil {
		return nil, model.NewError(model.KindNetwork, "invalid GraphQL response: "+err.Error())
	}
	if len(shell.Errors) > 0 {
		var msgs []string
		for _, e := range shell.Errors {
			msgs = append(msgs, e.Message)
		}
		return nil, model.NewError(model.KindNetwork, "GraphQL errors: "+strings.Join(msgs, "; "))
	}
	if shell.Data.Schema.Types == nil {
		return nil, model.NewError(model.KindNetwork, "introspection returned no __schema")
	}

	// 整理补全用结果
	res := &Result{}
	// 输出原始 schema JSON：用 shell.Data.Schema 序列化（保证结构化可用）
	schemaOut, _ := json.Marshal(shell.Data.Schema)
	res.SchemaJSON = string(schemaOut)

	// 找 Query/Mutation/Subscription type 的字段
	byName := map[string]FullType{}
	for _, t := range shell.Data.Schema.Types {
		byName[t.Name] = t
	}

	ops := func(rootName string) []FieldSummary {
		t, ok := byName[rootName]
		if !ok || t.Kind != "OBJECT" {
			return nil
		}
		out := make([]FieldSummary, 0, len(t.Fields))
		for _, f := range t.Fields {
			fs := FieldSummary{
				Name: f.Name, Description: f.Description,
				ReturnType: typeRefToString(f.Type),
				ReturnKind: namedTypeKind(f.Type),
			}
			if len(f.Args) > 0 {
				args := make([]struct {
					Name string `json:"name"`
					Type string `json:"type"`
				}, 0, len(f.Args))
				for _, arg := range f.Args {
					args = append(args, struct {
						Name string `json:"name"`
						Type string `json:"type"`
					}{Name: arg.Name, Type: typeRefToString(arg.Type)})
				}
				argsJSON, _ := json.Marshal(args)
				fs.Args = string(argsJSON)
			}
			out = append(out, fs)
		}
		return out
	}
	if shell.Data.Schema.QueryType != nil {
		res.Queries = ops(shell.Data.Schema.QueryType.Name)
	}
	if shell.Data.Schema.MutationType != nil {
		res.Mutations = ops(shell.Data.Schema.MutationType.Name)
	}
	if shell.Data.Schema.SubscriptionType != nil {
		res.Subscriptions = ops(shell.Data.Schema.SubscriptionType.Name)
	}
	return res, nil
}

func namedTypeKind(t *TypeRef) string {
	for t != nil && (t.Kind == "NON_NULL" || t.Kind == "LIST") {
		t = t.OfType
	}
	if t == nil {
		return ""
	}
	return t.Kind
}

// typeRefToString 把 TypeRef 渲染为 GraphQL 字符串（如 [Int!]!）
func typeRefToString(t *TypeRef) string {
	if t == nil {
		return ""
	}
	switch t.Kind {
	case "NON_NULL":
		return typeRefToString(t.OfType) + "!"
	case "LIST":
		return "[" + typeRefToString(t.OfType) + "]"
	default:
		return t.Name
	}
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
