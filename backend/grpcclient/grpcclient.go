// Package grpcclient 实现 gRPC 反射发现与动态 unary 调用（docs/protocols.md §4）。
// 优先 server reflection 拉取描述，用 protoreflect + dynamicpb 动态编解码，
// 无需为每个 proto 预生成代码。
package grpcclient

import (
	"context"
	"crypto/tls"
	"fmt"
	"sort"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	"apirequest/backend/model"
)

// ConnectConfig 连接配置
type ConnectConfig struct {
	Target    string `json:"target"` // host:port
	UseTLS    bool   `json:"useTls"`
	Insecure  bool   `json:"insecureTls,omitempty"` // TLS 但跳过校验
	TimeoutMs int    `json:"timeoutMs,omitempty"`   // 默认 15000
}

// MethodInfo 反射发现的方法
type MethodInfo struct {
	Service        string `json:"service"`  // 完整服务名
	Method         string `json:"method"`   // 方法名
	FullName       string `json:"fullName"` // /pkg.Service/Method
	ClientStream   bool   `json:"clientStream"`
	ServerStream   bool   `json:"serverStream"`
	InputExample   string `json:"inputExample"` // 入参消息的 JSON 骨架
}

// CallResult 调用结果
type CallResult struct {
	Response   string            `json:"response"` // JSON
	DurationMs int64             `json:"durationMs"`
	Headers    map[string]string `json:"headers,omitempty"`
	Trailers   map[string]string `json:"trailers,omitempty"`
}

func dial(ctx context.Context, cfg ConnectConfig) (*grpc.ClientConn, error) {
	var creds grpc.DialOption
	if cfg.UseTLS {
		tc := &tls.Config{InsecureSkipVerify: cfg.Insecure}
		creds = grpc.WithTransportCredentials(credentials.NewTLS(tc))
	} else {
		creds = grpc.WithTransportCredentials(insecure.NewCredentials())
	}
	conn, err := grpc.NewClient(cfg.Target, creds)
	if err != nil {
		return nil, model.WrapError(model.KindNetwork, err)
	}
	return conn, nil
}

func timeoutOf(cfg ConnectConfig) time.Duration {
	if cfg.TimeoutMs > 0 {
		return time.Duration(cfg.TimeoutMs) * time.Millisecond
	}
	return 15 * time.Second
}

// Discover 经 server reflection 列出全部服务与方法
func Discover(cfg ConnectConfig) ([]MethodInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeoutOf(cfg))
	defer cancel()

	conn, err := dial(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	files, serviceNames, err := fetchDescriptors(ctx, conn)
	if err != nil {
		return nil, err
	}

	var out []MethodInfo
	for _, svcName := range serviceNames {
		if strings.HasPrefix(svcName, "grpc.reflection.") || strings.HasPrefix(svcName, "grpc.health.") {
			continue // 基础设施服务不展示
		}
		desc, err := files.FindDescriptorByName(protoreflect.FullName(svcName))
		if err != nil {
			continue
		}
		svc, ok := desc.(protoreflect.ServiceDescriptor)
		if !ok {
			continue
		}
		methods := svc.Methods()
		for i := 0; i < methods.Len(); i++ {
			m := methods.Get(i)
			out = append(out, MethodInfo{
				Service:      svcName,
				Method:       string(m.Name()),
				FullName:     fmt.Sprintf("/%s/%s", svcName, m.Name()),
				ClientStream: m.IsStreamingClient(),
				ServerStream: m.IsStreamingServer(),
				InputExample: messageSkeleton(m.Input()),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FullName < out[j].FullName })
	if len(out) == 0 {
		return nil, model.NewError(model.KindNetwork,
			"no services discovered (server reflection enabled on target?)")
	}
	return out, nil
}

// Call 动态 unary 调用：JSON 入参 → protojson 编码 → 调用 → JSON 出参
func Call(cfg ConnectConfig, fullMethod, requestJSON string, headers map[string]string) (*CallResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeoutOf(cfg))
	defer cancel()

	conn, err := dial(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	files, _, err := fetchDescriptors(ctx, conn)
	if err != nil {
		return nil, err
	}

	// /pkg.Service/Method → 描述
	trimmed := strings.TrimPrefix(fullMethod, "/")
	slash := strings.LastIndex(trimmed, "/")
	if slash < 0 {
		return nil, model.NewError(model.KindValidation, "method must be /package.Service/Method")
	}
	svcName, methodName := trimmed[:slash], trimmed[slash+1:]
	desc, err := files.FindDescriptorByName(protoreflect.FullName(svcName))
	if err != nil {
		return nil, model.NewError(model.KindValidation, "service not found: "+svcName)
	}
	svc, ok := desc.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, model.NewError(model.KindValidation, "not a service: "+svcName)
	}
	m := svc.Methods().ByName(protoreflect.Name(methodName))
	if m == nil {
		return nil, model.NewError(model.KindValidation, "method not found: "+methodName)
	}
	if m.IsStreamingClient() || m.IsStreamingServer() {
		return nil, model.NewError(model.KindValidation, "streaming methods not supported yet (unary only)")
	}

	// 动态消息编解码
	reqMsg := dynamicpb.NewMessage(m.Input())
	if strings.TrimSpace(requestJSON) == "" {
		requestJSON = "{}"
	}
	if err := protojson.Unmarshal([]byte(requestJSON), reqMsg); err != nil {
		return nil, model.NewError(model.KindValidation, "request JSON does not match input message: "+err.Error())
	}
	respMsg := dynamicpb.NewMessage(m.Output())

	// metadata
	if len(headers) > 0 {
		pairs := make([]string, 0, len(headers)*2)
		for k, v := range headers {
			pairs = append(pairs, k, v)
		}
		ctx = metadata.AppendToOutgoingContext(ctx, pairs...)
	}

	var respHeader, respTrailer metadata.MD
	start := time.Now()
	err = conn.Invoke(ctx, fullMethod, reqMsg, respMsg,
		grpc.Header(&respHeader), grpc.Trailer(&respTrailer))
	duration := time.Since(start).Milliseconds()
	if err != nil {
		return nil, model.WrapError(model.KindNetwork, err)
	}

	out, err := protojson.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(respMsg)
	if err != nil {
		return nil, model.WrapError(model.KindNetwork, err)
	}
	return &CallResult{
		Response:   string(out),
		DurationMs: duration,
		Headers:    flattenMD(respHeader),
		Trailers:   flattenMD(respTrailer),
	}, nil
}

// fetchDescriptors 经 reflection v1 拉取全部文件描述并注册
func fetchDescriptors(ctx context.Context, conn *grpc.ClientConn) (*protoregistry.Files, []string, error) {
	client := grpc_reflection_v1.NewServerReflectionClient(conn)
	stream, err := client.ServerReflectionInfo(ctx)
	if err != nil {
		return nil, nil, model.WrapError(model.KindNetwork, err)
	}
	defer stream.CloseSend()

	// 1. 列服务
	if err := stream.Send(&grpc_reflection_v1.ServerReflectionRequest{
		MessageRequest: &grpc_reflection_v1.ServerReflectionRequest_ListServices{},
	}); err != nil {
		return nil, nil, model.WrapError(model.KindNetwork, err)
	}
	resp, err := stream.Recv()
	if err != nil {
		return nil, nil, model.NewError(model.KindNetwork,
			"server reflection unavailable: "+err.Error())
	}
	list := resp.GetListServicesResponse()
	if list == nil {
		return nil, nil, model.NewError(model.KindNetwork, "unexpected reflection response")
	}
	var serviceNames []string
	for _, s := range list.Service {
		serviceNames = append(serviceNames, s.Name)
	}

	// 2. 逐服务拉文件描述（含依赖），去重后注册
	fdMap := map[string]*descriptorpb.FileDescriptorProto{}
	for _, svc := range serviceNames {
		if strings.HasPrefix(svc, "grpc.reflection.") {
			continue
		}
		if err := stream.Send(&grpc_reflection_v1.ServerReflectionRequest{
			MessageRequest: &grpc_reflection_v1.ServerReflectionRequest_FileContainingSymbol{
				FileContainingSymbol: svc,
			},
		}); err != nil {
			return nil, nil, model.WrapError(model.KindNetwork, err)
		}
		resp, err := stream.Recv()
		if err != nil {
			return nil, nil, model.WrapError(model.KindNetwork, err)
		}
		fdResp := resp.GetFileDescriptorResponse()
		if fdResp == nil {
			continue
		}
		for _, raw := range fdResp.FileDescriptorProto {
			fd := &descriptorpb.FileDescriptorProto{}
			if err := proto.Unmarshal(raw, fd); err != nil {
				continue
			}
			fdMap[fd.GetName()] = fd
		}
	}

	fdSet := &descriptorpb.FileDescriptorSet{}
	for _, fd := range fdMap {
		fdSet.File = append(fdSet.File, fd)
	}
	files, err := protodesc.NewFiles(fdSet)
	if err != nil {
		return nil, nil, model.NewError(model.KindNetwork, "build descriptors: "+err.Error())
	}
	return files, serviceNames, nil
}

// messageSkeleton 生成入参 JSON 骨架（字段默认值），供编辑器起手
func messageSkeleton(md protoreflect.MessageDescriptor) string {
	msg := dynamicpb.NewMessage(md)
	// 填充标量字段默认零值让 protojson 输出字段名
	out, err := protojson.MarshalOptions{
		Multiline: true, Indent: "  ", EmitUnpopulated: true,
	}.Marshal(msg)
	if err != nil {
		return "{}"
	}
	return string(out)
}

func flattenMD(md metadata.MD) map[string]string {
	if len(md) == 0 {
		return nil
	}
	out := map[string]string{}
	for k, vs := range md {
		out[k] = strings.Join(vs, ", ")
	}
	return out
}
