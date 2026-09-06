// proto 文件直载：服务器未开启 server reflection 时，用户可直接指定 .proto 文件
// （protoparse 解析源文件，import 目录解析依赖）。proto 模式下 Discover 完全
// 离线——不拨号、不反射；Call 仍按需建立连接。
package grpcclient

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jhump/protoreflect/desc/protoparse"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"apirequest/backend/model"
)

// loadProtoFiles 解析 .proto 源文件为描述符集合。includeDirs 为额外 import 根目录；
// proto 文件自身所在目录始终参与解析（go 例行惯例）。
// 返回注册完成的文件集与全部服务全名。
func loadProtoFiles(protoFile string, includeDirs []string) (*protoregistry.Files, []string, error) {
	if _, err := os.Stat(protoFile); err != nil {
		return nil, nil, model.NewError(model.KindValidation, "proto file not readable: "+err.Error())
	}
	dir := filepath.Dir(protoFile)
	paths := append([]string{dir}, includeDirs...)

	parser := protoparse.Parser{
		ImportPaths:      paths,
		InferImportPaths: false,
	}
	// protoparse 把文件名按 ImportPaths 逐个解析（Windows 绝对路径会被二次拼接），
	// 因此只传文件自身相对其目录的名字——该目录是 ImportPaths[0]
	fds, err := parser.ParseFiles(filepath.Base(protoFile))
	if err != nil {
		return nil, nil, model.NewError(model.KindValidation, "parse proto: "+err.Error())
	}
	files := &protoregistry.Files{}
	for _, fd := range fds {
		if err := files.RegisterFile(fd.UnwrapFile()); err != nil {
			// 同名文件重复注册等：proto 模式下视为配置错误直接暴露
			return nil, nil, model.NewError(model.KindValidation, "register proto: "+err.Error())
		}
	}
	var services []string
	files.RangeFiles(func(f protoreflect.FileDescriptor) bool {
		svcs := f.Services()
		for i := 0; i < svcs.Len(); i++ {
			services = append(services, string(svcs.Get(i).FullName()))
		}
		return true
	})
	if len(services) == 0 {
		return nil, nil, model.NewError(model.KindValidation,
			fmt.Sprintf("no services declared in %s", filepath.Base(protoFile)))
	}
	return files, services, nil
}
