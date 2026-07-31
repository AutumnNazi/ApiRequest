package binding

import (
	"context"

	"apirequest/backend/graphql"
)

// GraphqlApi GraphQL 内省域（docs/protocols.md §5）
type GraphqlApi struct {
	ctx context.Context
}

// NewGraphqlApi 构造
func NewGraphqlApi() *GraphqlApi { return &GraphqlApi{} }

func (a *GraphqlApi) startup(ctx context.Context) { a.ctx = ctx }

// GraphqlIntrospect 向给定 endpoint 发起 introspection query，返回补全输入
func (a *GraphqlApi) GraphqlIntrospect(cfg graphql.IntrospectConfig) (*graphql.Result, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return graphql.Introspect(ctx, cfg)
}
