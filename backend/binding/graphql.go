package binding

import (
	"context"
	"net/http"

	"apirequest/backend/graphql"
)

// GraphqlApi GraphQL 内省域（docs/protocols.md §5）
type GraphqlApi struct {
	ctx    context.Context
	client *http.Client
}

// NewGraphqlApi 构造
func NewGraphqlApi(clients ...*http.Client) *GraphqlApi {
	client := http.DefaultClient
	if len(clients) > 0 && clients[0] != nil {
		client = clients[0]
	}
	return &GraphqlApi{client: client}
}

func (a *GraphqlApi) startup(ctx context.Context) { a.ctx = ctx }

// GraphqlIntrospect 向给定 endpoint 发起 introspection query，返回补全输入
func (a *GraphqlApi) GraphqlIntrospect(cfg graphql.IntrospectConfig) (*graphql.Result, error) {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return graphql.IntrospectWithClient(ctx, cfg, a.client)
}
