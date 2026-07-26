package runner

import (
	"testing"

	"apirequest/backend/model"
)

func TestParseDataFileCSV(t *testing.T) {
	rows, err := ParseDataFile("name,age\nalice,30\nbob,25", "csv")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0]["name"] != "alice" || rows[1]["age"] != "25" {
		t.Errorf("rows = %+v", rows)
	}
}

func TestParseDataFileJSON(t *testing.T) {
	rows, err := ParseDataFile(`[{"id": 1, "name": "x"}, {"id": 2, "name": "y"}]`, "json")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0]["id"] != "1" || rows[1]["name"] != "y" {
		t.Errorf("rows = %+v", rows)
	}
}

func TestParseDataFileErrors(t *testing.T) {
	if _, err := ParseDataFile("only-header", "csv"); err == nil {
		t.Error("csv without data rows should error")
	}
	if _, err := ParseDataFile("{not json array}", "json"); err == nil {
		t.Error("bad json should error")
	}
	if rows, err := ParseDataFile("", "csv"); err != nil || rows != nil {
		t.Error("empty content should return nil, nil")
	}
}

func TestFlattenOrdered(t *testing.T) {
	req := &model.HttpRequest{Method: "GET", Url: "https://x.io"}
	nodes := []model.Node{
		{Id: "col", Kind: "collection", Name: "c"},
		{Id: "f1", ParentId: "col", Kind: "folder", Name: "f1", SortOrder: 20},
		{Id: "r1", ParentId: "col", Kind: "request", Name: "r1", SortOrder: 10, Request: req},
		{Id: "r2", ParentId: "f1", Kind: "request", Name: "r2", SortOrder: 10, Request: req},
		{Id: "r3", ParentId: "f1", Kind: "request", Name: "r3", SortOrder: 5, Request: req},
		{Id: "other", Kind: "collection", Name: "other"},
		{Id: "r4", ParentId: "other", Kind: "request", Name: "r4", Request: req},
	}
	out := FlattenOrdered("col", nodes)
	if len(out) != 3 {
		t.Fatalf("len = %d, want 3 (r4 belongs to another collection)", len(out))
	}
	// 顺序：r1(排前) → folder f1 内 r3(排前) → r2
	want := []string{"r1", "r3", "r2"}
	for i, n := range out {
		if n.Id != want[i] {
			t.Errorf("order[%d] = %s, want %s", i, n.Id, want[i])
		}
	}
}
