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
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"apirequest/backend/model"
)

const schemaVersion = 1

const maxMirrorJSONSize = 32 << 20

const (
	maxMirrorNodes         = 100_000
	maxMirrorEntriesPerDir = 100_000
	maxMirrorDepth         = 256
)

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
	if err := validateExportTree(collection, children); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := requireMirrorDirectory(dir); err != nil {
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

	var walk func(parentId, parentDir string, depth int) error
	walk = func(parentId, parentDir string, depth int) error {
		if depth > maxMirrorDepth {
			return fmt.Errorf("mirror exceeds maximum directory depth %d", maxMirrorDepth)
		}
		reservedName := "_folder.json"
		if parentId == collection.Id {
			reservedName = "collection.json"
		}
		entryNames := allocateEntryNames(byParent[parentId], reservedName)
		for _, n := range byParent[parentId] {
			switch n.Kind {
			case "folder":
				sub := filepath.Join(parentDir, entryNames[n.Id])
				if err := createMirrorDirectory(sub); err != nil {
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
				if err := walk(n.Id, sub, depth+1); err != nil {
					return err
				}
			case "request":
				if n.Request == nil {
					continue
				}
				rf := requestFile{Name: n.Name, SortOrder: n.SortOrder, Request: *n.Request}
				fp := filepath.Join(parentDir, entryNames[n.Id])
				if err := writeJSON(fp, rf); err != nil {
					return err
				}
				expected[fp] = true
			}
		}
		return nil
	}
	if err := walk(collection.Id, dir, 0); err != nil {
		return err
	}

	// 删除不再存在的镜像文件（只动我们认识的后缀，不碰用户其它文件）
	if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		name := info.Name()
		if (strings.HasSuffix(name, ".request.json") || name == "_folder.json") && !expected[path] {
			return os.Remove(path)
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func validateExportTree(collection model.Node, children []model.Node) error {
	if strings.TrimSpace(collection.Id) == "" {
		return errors.New("mirror collection id is required")
	}
	if collection.Kind != "collection" {
		return fmt.Errorf("mirror root kind must be collection, got %q", collection.Kind)
	}
	if len(children) > maxMirrorNodes {
		return fmt.Errorf("mirror exceeds %d node limit", maxMirrorNodes)
	}

	kinds := make(map[string]string, len(children)+1)
	kinds[collection.Id] = collection.Kind
	byParent := make(map[string][]string, len(children))
	for _, node := range children {
		if strings.TrimSpace(node.Id) == "" {
			return errors.New("mirror node id is required")
		}
		if _, duplicate := kinds[node.Id]; duplicate {
			return fmt.Errorf("mirror contains duplicate node id %q", node.Id)
		}
		switch node.Kind {
		case "folder":
		case "request":
			if node.Request == nil {
				return fmt.Errorf("mirror request node %q has no request data", node.Id)
			}
		default:
			return fmt.Errorf("mirror node %q has unsupported kind %q", node.Id, node.Kind)
		}
		kinds[node.Id] = node.Kind
		byParent[node.ParentId] = append(byParent[node.ParentId], node.Id)
	}
	for _, node := range children {
		parentKind, exists := kinds[node.ParentId]
		if !exists {
			return fmt.Errorf("mirror node %q references missing parent %q", node.Id, node.ParentId)
		}
		if parentKind != "collection" && parentKind != "folder" {
			return fmt.Errorf("mirror node %q has invalid parent kind %q", node.Id, parentKind)
		}
	}

	visited := map[string]struct{}{collection.Id: {}}
	queue := []string{collection.Id}
	for len(queue) > 0 {
		parent := queue[0]
		queue = queue[1:]
		for _, child := range byParent[parent] {
			if _, exists := visited[child]; exists {
				continue
			}
			visited[child] = struct{}{}
			queue = append(queue, child)
		}
	}
	if len(visited) != len(children)+1 {
		return errors.New("mirror tree contains a cycle or nodes unreachable from the collection root")
	}
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
	nodeCount := 0
	nextId := func() string {
		seq++
		return fmt.Sprintf("import-%d", seq)
	}

	var walkDir func(dirPath, parentId string, depth int) error
	walkDir = func(dirPath, parentId string, depth int) error {
		if depth > maxMirrorDepth {
			return fmt.Errorf("mirror exceeds maximum directory depth %d", maxMirrorDepth)
		}
		entries, err := readDirBounded(dirPath)
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
				nodeCount++
				if nodeCount > maxMirrorNodes {
					return fmt.Errorf("mirror exceeds %d node limit", maxMirrorNodes)
				}
				folder := model.Node{
					Id: nextId(), ParentId: parentId, Kind: "folder",
					Name: fm.Name, SortOrder: fm.SortOrder,
					Variables: fm.Variables, PreScript: fm.PreScript,
					TestScript: fm.TestScript, Auth: fm.Auth,
				}
				children = append(children, folder)
				if err := walkDir(full, folder.Id, depth+1); err != nil {
					return err
				}
			} else if strings.HasSuffix(e.Name(), ".request.json") {
				var rf requestFile
				if err := readJSON(full, &rf); err != nil {
					return fmt.Errorf("read %s: %w", e.Name(), err)
				}
				nodeCount++
				if nodeCount > maxMirrorNodes {
					return fmt.Errorf("mirror exceeds %d node limit", maxMirrorNodes)
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
	err = walkDir(dir, collection.Id, 0)
	// Keep depth-first parent-before-child order. ImportCommit needs every
	// parent ID mapped before its descendants; SortOrder is persisted on nodes
	// and controls display order independently of insertion order.
	return collection, children, err
}

func readDirBounded(path string) ([]os.DirEntry, error) {
	dir, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(maxMirrorEntriesPerDir + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(entries) > maxMirrorEntriesPerDir {
		return nil, fmt.Errorf("mirror directory exceeds %d entry limit: %s", maxMirrorEntriesPerDir, path)
	}
	return entries, nil
}

// slug 文件名安全化：兼容 Windows/macOS/Linux，并确保结果只是一个路径组件。
func slug(name string) string {
	var builder strings.Builder
	for _, r := range strings.TrimSpace(name) {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"/\|?*`, r) {
			builder.WriteByte('-')
			continue
		}
		builder.WriteRune(r)
	}
	out := strings.Trim(builder.String(), " .")
	if out == "" {
		out = "unnamed"
	}
	if isWindowsDeviceName(out) {
		out = "_" + out
	}
	out = truncateUTF8(out, 160)
	out = strings.TrimRight(out, " .")
	if out == "" {
		return "unnamed"
	}
	return out
}

func allocateEntryNames(nodes []model.Node, reservedName string) map[string]string {
	candidates := make(map[string]string, len(nodes))
	counts := map[string]int{entryNameKey(reservedName): 1}
	for _, node := range nodes {
		var candidate string
		switch node.Kind {
		case "folder":
			candidate = slug(node.Name)
		case "request":
			if node.Request == nil {
				continue
			}
			candidate = slug(node.Name) + ".request.json"
		default:
			continue
		}
		candidates[node.Id] = candidate
		counts[entryNameKey(candidate)]++
	}
	used := map[string]struct{}{entryNameKey(reservedName): {}}
	for _, node := range nodes {
		candidate, ok := candidates[node.Id]
		if !ok {
			continue
		}
		key := entryNameKey(candidate)
		_, alreadyUsed := used[key]
		if counts[key] > 1 || alreadyUsed {
			token := pathIdentity(node.Id)
			for attempt := 0; ; attempt++ {
				disambiguated := disambiguateEntryName(candidate, node.Kind, token, attempt)
				disambiguatedKey := entryNameKey(disambiguated)
				_, generatedCollision := used[disambiguatedKey]
				if counts[disambiguatedKey] == 0 && !generatedCollision {
					candidate = disambiguated
					key = disambiguatedKey
					break
				}
			}
		}
		candidates[node.Id] = candidate
		used[key] = struct{}{}
	}
	return candidates
}

func disambiguateEntryName(candidate, kind, token string, attempt int) string {
	suffix := "--" + token
	if attempt > 0 {
		suffix += fmt.Sprintf("-%d", attempt)
	}
	if kind == "request" {
		return strings.TrimSuffix(candidate, ".request.json") + suffix + ".request.json"
	}
	return candidate + suffix
}

func entryNameKey(value string) string { return strings.ToLower(value) }

func pathIdentity(id string) string {
	sum := sha256.Sum256([]byte(id))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func isWindowsDeviceName(value string) bool {
	base := strings.ToUpper(strings.SplitN(value, ".", 2)[0])
	switch base {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	return len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9'
}

func truncateUTF8(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(value[cut]) {
		cut--
	}
	return value[:cut]
}

func requireMirrorDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("mirror path is not a regular directory: %s", path)
	}
	return nil
}

func createMirrorDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.Mkdir(path, 0o755)
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("mirror path is not a regular directory: %s", path)
	}
	return nil
}

func writeJSON(path string, v any) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("mirror JSON path is not a regular file: %s", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n') // Git 友好：文件以换行结尾
	return os.WriteFile(path, b, 0o644)
}

func readJSON(path string, v any) error {
	pathInfo, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() {
		return fmt.Errorf("mirror JSON is not a regular file: %s", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("mirror JSON is not a regular file: %s", path)
	}
	if info.Size() > maxMirrorJSONSize {
		return fmt.Errorf("mirror JSON exceeds %d MiB limit: %s", maxMirrorJSONSize>>20, path)
	}
	b, err := io.ReadAll(io.LimitReader(file, maxMirrorJSONSize+1))
	if err != nil {
		return err
	}
	if len(b) > maxMirrorJSONSize {
		return fmt.Errorf("mirror JSON exceeds %d MiB limit: %s", maxMirrorJSONSize>>20, path)
	}
	return json.Unmarshal(b, v)
}
