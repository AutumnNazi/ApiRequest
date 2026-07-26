// Package mirror 实现集合的 Git 友好文件镜像（docs/decisions.md OPEN-004：JSON 格式）。
// 布局：
//
//	<dir>/collection.json          集合元信息（名称/变量/脚本/schemaVersion）
//	<dir>/<folder-name>/_folder.json  文件夹元信息（可嵌套）
//	<dir>/.../<request-name>.request.json  单请求单文件（Git diff 友好）
//
// 相对路径统一 '/'；文件名经 slug 处理避免非法字符（docs/data-model.md 路径约定）。
package mirror

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"apirequest/backend/model"
)

const schemaVersion = 1

// collectionMeta collection.json 内容
type collectionMeta struct {
	SchemaVersion int              `json:"schemaVersion"`
	Name          string           `json:"name"`
	Variables     []model.Variable `json:"variables,omitempty"`
	PreScript     string           `json:"preScript,omitempty"`
	TestScript    string           `json:"testScript,omitempty"`
	Auth          *model.Auth      `json:"auth,omitempty"`
}

// folderMeta _folder.json 内容
type folderMeta struct {
	Name       string           `json:"name"`
	SortOrder  float64          `json:"sortOrder"`
	Variables  []model.Variable `json:"variables,omitempty"`
	PreScript  string           `json:"preScript,omitempty"`
	TestScript string           `json:"testScript,omitempty"`
	Auth       *model.Auth      `json:"auth,omitempty"`
}

// requestFile *.request.json 内容
type requestFile struct {
	Name      string            `json:"name"`
	SortOrder float64           `json:"sortOrder"`
	Request   model.HttpRequest `json:"request"`
}

// Export 把集合树写为目录镜像。dir 会被创建；已存在的 .request.json/_folder.json
// 若不再对应节点则删除（保持镜像与集合一致，利于 Git diff）。
func Export(dir string, collection model.Node, children []model.Node) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// 根元信息
	meta := collectionMeta{
		SchemaVersion: schemaVersion,
		Name:          collection.Name,
		Variables:     collection.Variables,
		PreScript:     collection.PreScript,
		TestScript:    collection.TestScript,
		Auth:          collection.Auth,
	}
	if err := writeJSON(filepath.Join(dir, "collection.json"), meta); err != nil {
		return err
	}

	byParent := map[string][]model.Node{}
	for _, n := range children {
		byParent[n.ParentId] = append(byParent[n.ParentId], n)
	}
	// 清理陈旧文件前先收集期望路径
	expected := map[string]bool{filepath.Join(dir, "collection.json"): true}

	var walk func(parentId, parentDir string) error
	walk = func(parentId, parentDir string) error {
		for _, n := range byParent[parentId] {
			switch n.Kind {
			case "folder":
				sub := filepath.Join(parentDir, slug(n.Name))
				if err := os.MkdirAll(sub, 0o755); err != nil {
					return err
				}
				fm := folderMeta{
					Name: n.Name, SortOrder: n.SortOrder,
					Variables: n.Variables, PreScript: n.PreScript,
					TestScript: n.TestScript, Auth: n.Auth,
				}
				fp := filepath.Join(sub, "_folder.json")
				if err := writeJSON(fp, fm); err != nil {
					return err
				}
				expected[fp] = true
				if err := walk(n.Id, sub); err != nil {
					return err
				}
			case "request":
				if n.Request == nil {
					continue
				}
				rf := requestFile{Name: n.Name, SortOrder: n.SortOrder, Request: *n.Request}
				fp := filepath.Join(parentDir, slug(n.Name)+".request.json")
				if err := writeJSON(fp, rf); err != nil {
					return err
				}
				expected[fp] = true
			}
		}
		return nil
	}
	if err := walk(collection.Id, dir); err != nil {
		return err
	}

	// 删除不再存在的镜像文件（只动我们认识的后缀，不碰用户其它文件）
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		name := info.Name()
		if (strings.HasSuffix(name, ".request.json") || name == "_folder.json") && !expected[path] {
			os.Remove(path)
		}
		return nil
	})
	return nil
}

// Import 从镜像目录读回集合树（id 为占位，落库时重新生成——同 convert.ImportResult 语义）
func Import(dir string) (collection model.Node, children []model.Node, err error) {
	metaPath := filepath.Join(dir, "collection.json")
	var meta collectionMeta
	if err = readJSON(metaPath, &meta); err != nil {
		return collection, nil, fmt.Errorf("read collection.json: %w", err)
	}
	if meta.SchemaVersion > schemaVersion {
		return collection, nil, fmt.Errorf("mirror schemaVersion %d newer than supported %d", meta.SchemaVersion, schemaVersion)
	}
	collection = model.Node{
		Id: "import-root", Kind: "collection", Name: meta.Name,
		Variables: meta.Variables, PreScript: meta.PreScript,
		TestScript: meta.TestScript, Auth: meta.Auth,
	}
	seq := 0
	nextId := func() string {
		seq++
		return fmt.Sprintf("import-%d", seq)
	}

	var walkDir func(dirPath, parentId string) error
	walkDir = func(dirPath, parentId string) error {
		entries, err := os.ReadDir(dirPath)
		if err != nil {
			return err
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, e := range entries {
			full := filepath.Join(dirPath, e.Name())
			if e.IsDir() {
				fmPath := filepath.Join(full, "_folder.json")
				var fm folderMeta
				if readJSON(fmPath, &fm) != nil {
					continue // 无 _folder.json 的目录不是镜像的一部分
				}
				folder := model.Node{
					Id: nextId(), ParentId: parentId, Kind: "folder",
					Name: fm.Name, SortOrder: fm.SortOrder,
					Variables: fm.Variables, PreScript: fm.PreScript,
					TestScript: fm.TestScript, Auth: fm.Auth,
				}
				children = append(children, folder)
				if err := walkDir(full, folder.Id); err != nil {
					return err
				}
			} else if strings.HasSuffix(e.Name(), ".request.json") {
				var rf requestFile
				if err := readJSON(full, &rf); err != nil {
					return fmt.Errorf("read %s: %w", e.Name(), err)
				}
				req := rf.Request
				children = append(children, model.Node{
					Id: nextId(), ParentId: parentId, Kind: "request",
					Name: rf.Name, SortOrder: rf.SortOrder, Request: &req,
				})
			}
		}
		return nil
	}
	err = walkDir(dir, collection.Id)
	// 按 sortOrder 稳定排序（Import 时目录序≠显示序）
	sort.SliceStable(children, func(i, j int) bool {
		if children[i].ParentId != children[j].ParentId {
			return children[i].ParentId < children[j].ParentId
		}
		return children[i].SortOrder < children[j].SortOrder
	})
	return collection, children, err
}

// slug 文件名安全化：替换路径非法字符，保留中文与常见符号
func slug(name string) string {
	replacer := strings.NewReplacer(
		"/", "-", "\\", "-", ":", "-", "*", "-", "?", "-",
		"\"", "'", "<", "(", ">", ")", "|", "-",
	)
	out := strings.TrimSpace(replacer.Replace(name))
	if out == "" {
		out = "unnamed"
	}
	return out
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n') // Git 友好：文件以换行结尾
	return os.WriteFile(path, b, 0o644)
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
