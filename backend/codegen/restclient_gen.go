package codegen

import (
	"fmt"
	"strings"

	"apirequest/backend/model"
)

// restClientGen 生成 VS Code REST Client / JetBrains HTTP client 的 .http 请求块
type restClientGen struct{}

func (restClientGen) Id() string   { return "restclient" }
func (restClientGen) Name() string { return "REST Client (.http)" }

func (restClientGen) Generate(req model.HttpRequest) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s %s\n", req.Method, fullUrl(req))
	for _, h := range enabledHeaders(req) {
		fmt.Fprintf(&sb, "%s: %s\n", h.Key, h.Value)
	}
	if text, _, ok := bodyText(req); ok && strings.TrimSpace(text) != "" {
		fmt.Fprintf(&sb, "\n%s\n", text)
	}
	return sb.String()
}

func init() { Register(restClientGen{}) }
