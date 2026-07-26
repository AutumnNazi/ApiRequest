package binding

import (
	"apirequest/backend/grpcclient"
)

// GrpcApi gRPC 反射发现与动态调用域（docs/protocols.md §4）
type GrpcApi struct{}

// NewGrpcApi 构造
func NewGrpcApi() *GrpcApi { return &GrpcApi{} }

// GrpcDiscover 经 server reflection 列出服务与方法
func (a *GrpcApi) GrpcDiscover(cfg grpcclient.ConnectConfig) ([]grpcclient.MethodInfo, error) {
	return grpcclient.Discover(cfg)
}

// GrpcCall 动态 unary 调用
func (a *GrpcApi) GrpcCall(cfg grpcclient.ConnectConfig, fullMethod, requestJSON string, headers map[string]string) (*grpcclient.CallResult, error) {
	return grpcclient.Call(cfg, fullMethod, requestJSON, headers)
}
