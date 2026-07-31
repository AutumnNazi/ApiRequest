package mirror

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"apirequest/backend/model"
)

func sampleTree() (model.Node, []model.Node) {
	col := model.Node{
		Id: "col", Kind: "collection", Name: "My API",
		Variables: []model.Variable{{Key: "base", Value: "https://x.io", Type: "default", Enabled: true}},
		PreScript: "// col pre",
	}
	req := func(id, parent, name string, order float64) model.Node {
		return model.Node{
			Id: id, ParentId: parent, Kind: "request", Name: name, SortOrder: order,
			Request: &model.HttpRequest{
				Method: "GET", Url: "{{base}}/" + name,
				Settings: model.DefaultSettings(),
			},
		}
	}
	children := []model.Node{
		{Id: "f1", ParentId: "col", Kind: "folder", Name: "Users", SortOrder: 10},
		req("r1", "f1", "list", 10),
		req("r2", "f1", "detail", 20),
		req("r3", "col", "health", 20),
	}
	return col, children
}

func TestExportImportRoundtrip(t *testing.T) {
	dir := t.TempDir()
	col, children := sampleTree()

	if err := Export(dir, col, children); err != nil {
		t.Fatalf("export: %v", err)
	}
	// 布局检查
	for _, p := range []string{
		"collection.json",
		"Users/_folder.json",
		"Users/list.request.json",
		"Users/detail.request.json",
		"health.request.json",
	} {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}

	col2, children2, err := Import(dir)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if col2.Name != "My API" || col2.PreScript != "// col pre" ||
		len(col2.Variables) != 1 || col2.Variables[0].Key != "base" {
		t.Errorf("collection = %+v", col2)
	}
	folders, requests := 0, 0
	var folderId string
	for _, n := range children2 {
		if n.Kind == "folder" {
			folders++
			folderId = n.Id
		} else {
			requests++
		}
	}
	if folders != 1 || requests != 3 {
		t.Fatalf("folders=%d requests=%d", folders, requests)
	}
	// 层级还原：list/detail 应挂在 folder 下
	inFolder := 0
	for _, n := range children2 {
		if n.Kind == "request" && n.ParentId == folderId {
			inFolder++
			if n.Request.Url != "{{base}}/list" && n.Request.Url != "{{base}}/detail" {
				t.Errorf("url = %s", n.Request.Url)
			}
		}
	}
	if inFolder != 2 {
		t.Errorf("requests in folder = %d, want 2", inFolder)
	}
}

func TestExportPrunesStale(t *testing.T) {
	dir := t.TempDir()
	col, children := sampleTree()
	if err := Export(dir, col, children); err != nil {
		t.Fatal(err)
	}
	// 删除一个请求后重导出：镜像文件应被清理
	if err := Export(dir, col, children[:len(children)-1]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "health.request.json")); !os.IsNotExist(err) {
		t.Error("stale request file should be pruned")
	}
	// 用户自己的文件不应被动
	userFile := filepath.Join(dir, "NOTES.md")
	os.WriteFile(userFile, []byte("keep me"), 0o644)
	Export(dir, col, children)
	if _, err := os.Stat(userFile); err != nil {
		t.Error("user file should be preserved")
	}
}

func TestExportRejectsInvalidTreesBeforeCreatingTarget(t *testing.T) {
	collection, _ := sampleTree()
	tests := []struct {
		name       string
		collection model.Node
		children   []model.Node
	}{
		{name: "missing collection id", collection: model.Node{Kind: "collection"}},
		{name: "wrong root kind", collection: model.Node{Id: "root", Kind: "folder"}},
		{
			name:       "missing parent",
			collection: collection,
			children: []model.Node{{
				Id: "orphan", ParentId: "missing", Kind: "folder", Name: "orphan",
			}},
		},
		{
			name:       "request without data",
			collection: collection,
			children: []model.Node{{
				Id: "request", ParentId: collection.Id, Kind: "request", Name: "request",
			}},
		},
		{
			name:       "folder cycle",
			collection: collection,
			children: []model.Node{
				{Id: "first", ParentId: "second", Kind: "folder", Name: "first"},
				{Id: "second", ParentId: "first", Kind: "folder", Name: "second"},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "mirror")
			if err := Export(target, test.collection, test.children); err == nil {
				t.Fatal("invalid mirror tree was accepted")
			}
			if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid export created target directory: %v", err)
			}
		})
	}
}

func TestSlugSanitizes(t *testing.T) {
	dir := t.TempDir()
	col := model.Node{Id: "col", Kind: "collection", Name: "c"}
	children := []model.Node{{
		Id: "r1", ParentId: "col", Kind: "request", Name: `GET /users?id=<1>|x`, SortOrder: 1,
		Request: &model.HttpRequest{Method: "GET", Url: "https://x.io", Settings: model.DefaultSettings()},
	}}
	if err := Export(dir, col, children); err != nil {
		t.Fatalf("export with special chars: %v", err)
	}
	_, children2, err := Import(dir)
	if err != nil || len(children2) != 1 {
		t.Fatalf("import: %v, n=%d", err, len(children2))
	}
	// 名称保留原样（存于 JSON 内），只有文件名被 slug
	if children2[0].Name != `GET /users?id=<1>|x` {
		t.Errorf("name = %s", children2[0].Name)
	}
}

func TestExportPreservesCollidingAndPathLikeNames(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "mirror")
	collection := model.Node{Id: "col", Kind: "collection", Name: "c"}
	request := func(id, name, url string) model.Node {
		return model.Node{
			Id: id, ParentId: collection.Id, Kind: "request", Name: name,
			Request: &model.HttpRequest{Method: "GET", Url: url, Settings: model.DefaultSettings()},
		}
	}
	children := []model.Node{
		{Id: "folder-dot", ParentId: collection.Id, Kind: "folder", Name: "."},
		{Id: "folder-dotdot", ParentId: collection.Id, Kind: "folder", Name: ".."},
		{Id: "folder-meta", ParentId: collection.Id, Kind: "folder", Name: "collection.json"},
		request("request-upper", "Status", "https://upper.test"),
		request("request-lower", "status", "https://lower.test"),
	}
	if err := Export(dir, collection, children); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(parent, "_folder.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path-like folder escaped mirror root: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	folders, requests := 0, 0
	for _, entry := range entries {
		if entry.IsDir() {
			folders++
		} else if strings.HasSuffix(entry.Name(), ".request.json") {
			requests++
		}
	}
	if folders != 3 || requests != 2 {
		t.Fatalf("exported folders=%d requests=%d entries=%v", folders, requests, entries)
	}

	_, imported, err := Import(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]int{}
	urls := map[string]bool{}
	for _, node := range imported {
		names[node.Name]++
		if node.Request != nil {
			urls[node.Request.Url] = true
		}
	}
	for _, name := range []string{".", "..", "collection.json", "Status", "status"} {
		if names[name] != 1 {
			t.Fatalf("name %q count = %d, imported = %+v", name, names[name], imported)
		}
	}
	if !urls["https://upper.test"] || !urls["https://lower.test"] {
		t.Fatalf("colliding request contents were lost: %v", urls)
	}
}

func TestSlugHandlesWindowsNamesAndUTF8Length(t *testing.T) {
	for _, name := range []string{"CON", "nul.txt", "COM1", "LPT9.log"} {
		if got := slug(name); !strings.HasPrefix(got, "_") {
			t.Fatalf("reserved name %q became %q", name, got)
		}
	}
	got := slug(strings.Repeat("界", 100))
	if len(got) > 160 || !utf8.ValidString(got) {
		t.Fatalf("long UTF-8 slug has %d bytes and valid=%v", len(got), utf8.ValidString(got))
	}
}

func TestImportMissingMeta(t *testing.T) {
	if _, _, err := Import(t.TempDir()); err == nil {
		t.Error("import from empty dir should fail")
	}
}

func TestImportKeepsParentsBeforeDescendants(t *testing.T) {
	dir := t.TempDir()
	col, children := sampleTree()
	if err := Export(dir, col, children); err != nil {
		t.Fatal(err)
	}
	_, imported, err := Import(dir)
	if err != nil {
		t.Fatal(err)
	}
	positions := make(map[string]int, len(imported))
	for i, node := range imported {
		positions[node.Id] = i
	}
	for i, node := range imported {
		if parentPosition, ok := positions[node.ParentId]; ok && parentPosition >= i {
			t.Fatalf("parent %q appears after child %q: %+v", node.ParentId, node.Id, imported)
		}
	}
}

func TestImportRejectsOversizedJSON(t *testing.T) {
	dir := t.TempDir()
	file, err := os.Create(filepath.Join(dir, "collection.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxMirrorJSONSize + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Import(dir); err == nil {
		t.Fatal("oversized mirror JSON was accepted")
	}
}
