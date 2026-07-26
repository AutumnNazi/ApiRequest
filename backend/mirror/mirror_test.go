package mirror

import (
	"os"
	"path/filepath"
	"testing"

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

func TestImportMissingMeta(t *testing.T) {
	if _, _, err := Import(t.TempDir()); err == nil {
		t.Error("import from empty dir should fail")
	}
}
