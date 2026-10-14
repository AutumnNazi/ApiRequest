package storage

import (
	"testing"

	"apirequest/backend/model"
)

// ImportNodeTree 必须带回 占位 id → 新 id 映射：导入器附带的关联数据
// （如 OpenAPI 响应示例挂占位 requestId）落库时要用它换算真实节点 id。
func TestImportNodeTreeReturnsIdMapping(t *testing.T) {
	s := openTestStore(t)
	ws, err := s.EnsureDefaultWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	collection := model.Node{Id: "ph-root", Kind: "collection", Name: "imported"}
	children := []model.Node{
		{Id: "ph-req", ParentId: "ph-root", Kind: "request", Name: "get user",
			Request: &model.HttpRequest{Method: "GET", Url: "https://x.test/u", Settings: model.DefaultSettings()}},
		{Id: "ph-folder", ParentId: "ph-root", Kind: "folder", Name: "group"},
		{Id: "ph-deep", ParentId: "ph-folder", Kind: "request", Name: "deep",
			Request: &model.HttpRequest{Method: "POST", Url: "https://x.test/d", Settings: model.DefaultSettings()}},
	}
	saved, mapping, err := s.ImportNodeTree(ws.Id, collection, children)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if mapping == nil || len(mapping) != 4 {
		t.Fatalf("mapping = %v, want 4 entries", mapping)
	}
	for _, ph := range []string{"ph-root", "ph-req", "ph-folder", "ph-deep"} {
		if mapping[ph] == "" || mapping[ph] == ph {
			t.Fatalf("mapping[%q] = %q, want a fresh id", ph, mapping[ph])
		}
	}
	if saved.Id != mapping["ph-root"] {
		t.Fatalf("collection id %q != mapping[ph-root] %q", saved.Id, mapping["ph-root"])
	}
	// 映射指向真实落库节点
	got, err := s.GetNode(ws.Id, mapping["ph-req"])
	if err != nil || got.Name != "get user" {
		t.Fatalf("mapped node lookup: %v %+v", err, got)
	}
}


