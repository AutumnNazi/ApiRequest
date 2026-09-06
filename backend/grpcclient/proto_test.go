package grpcclient

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func writeProto(t *testing.T, dir, name, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const protoMain = `syntax = "proto3";
package demo;

import "common/base.proto";

option go_package = "example.com/demo";

message HelloRequest { string name = 1; }
message HelloReply { string message = 1; common.Trace trace = 2; }

service Greeter {
  rpc SayHello (HelloRequest) returns (HelloReply);
  rpc Chat (stream HelloRequest) returns (stream HelloReply);
}
`

const protoCommon = `syntax = "proto3";
package common;

option go_package = "example.com/common";

message Trace { string id = 1; }
`

func TestLoadProtoFiles(t *testing.T) {
	dir := t.TempDir()
	writeProto(t, dir, "demo.proto", protoMain)
	// import 目标在子目录，经 ImportPaths 解析
	commonDir := filepath.Join(dir, "common")
	writeProto(t, commonDir, "base.proto", protoCommon)

	files, services, err := loadProtoFiles(filepath.Join(dir, "demo.proto"), nil)
	if err != nil {
		t.Fatalf("loadProtoFiles: %v", err)
	}
	found := false
	for _, s := range services {
		if s == "demo.Greeter" {
			found = true
		}
	}
	if !found {
		t.Fatalf("services = %v, want demo.Greeter", services)
	}
	desc, err := files.FindDescriptorByName("demo.Greeter")
	if err != nil {
		t.Fatalf("find service: %v", err)
	}
	svc, ok := desc.(protoreflect.ServiceDescriptor)
	if !ok {
		t.Fatalf("descriptor is %T", desc)
	}
	methods := svc.Methods()
	if methods.Len() != 2 {
		t.Fatalf("methods = %d", methods.Len())
	}
	chat := methods.ByName("Chat")
	if !chat.IsStreamingClient() || !chat.IsStreamingServer() {
		t.Fatal("Chat must be bidi streaming")
	}
	// 跨文件 import 的消息类型可用（骨架生成不报错）
	if sk := messageSkeleton(chat.Input()); !strings.Contains(sk, "name") {
		t.Fatalf("skeleton = %s", sk)
	}
}

func TestLoadProtoFilesExplicitImportDirs(t *testing.T) {
	dir := t.TempDir()
	writeProto(t, dir, "demo.proto", protoMain)
	// import 目标放在无关目录：只有显式提供 include 目录才能解析
	commonRoot := t.TempDir()
	writeProto(t, filepath.Join(commonRoot, "common"), "base.proto", protoCommon)

	// import "common/base.proto" → include 根目录是 common/ 的父目录
	if _, _, err := loadProtoFiles(filepath.Join(dir, "demo.proto"), []string{commonRoot}); err != nil {
		t.Fatalf("loadProtoFiles with import dir: %v", err)
	}
	if _, _, err := loadProtoFiles(filepath.Join(dir, "demo.proto"), nil); err == nil {
		t.Fatal("expected import resolution error")
	} else if !strings.Contains(err.Error(), "base.proto") {
		t.Fatalf("err = %v, want mention of base.proto", err)
	}
}

// proto 模式下 Discover 不依赖 server reflection：目标不可达也能列出方法
func TestDiscoverWithProtoFileSkipsReflection(t *testing.T) {
	dir := t.TempDir()
	writeProto(t, dir, "demo.proto", protoMain)
	commonDir := filepath.Join(dir, "common")
	writeProto(t, commonDir, "base.proto", protoCommon)

	cfg := ConnectConfig{
		Target:    "127.0.0.1:1", // 不可达
		TimeoutMs: 500,
		ProtoFile: filepath.Join(dir, "demo.proto"),
	}
	methods, err := Discover(cfg)
	if err != nil {
		t.Fatalf("discover with proto: %v", err)
	}
	if len(methods) != 2 {
		t.Fatalf("methods = %d", len(methods))
	}
	fullNames := map[string]bool{}
	for _, m := range methods {
		fullNames[m.FullName] = true
	}
	if !fullNames["/demo.Greeter/SayHello"] || !fullNames["/demo.Greeter/Chat"] {
		t.Fatalf("fullNames = %v", fullNames)
	}
}
