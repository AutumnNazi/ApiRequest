package binding

import (
	"context"
	"net/http"
	"time"

	"apirequest/backend/graphql"
)

// maxIntrospectDuration 内省兜底超时：共享 client 本身无超时（app.go 传入 0），
// 不加这层的话前端 Promise 会因无响应端点永久挂起
const maxIntrospectDuration = 30 * time.Second

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
	ctx, cancel := context.WithTimeout(ctx, maxIntrospectDuration)
	defer cancel()
	return graphql.IntrospectWithClient(ctx, cfg, a.client)
}
