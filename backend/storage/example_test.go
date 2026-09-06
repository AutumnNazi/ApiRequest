package storage

import (
	"testing"

	"apirequest/backend/model"
)

func TestExampleMockScriptRoundTrip(t *testing.T) {
	s := openStoreWithMemoryKeyring(t, t.TempDir(), &memoryKeyring{values: map[string]string{}})
	ws, _ := s.EnsureDefaultWorkspace()
	node, err := s.UpsertNode(model.Node{
		WorkspaceId: ws.Id, Kind: "collection", Name: "c",
	})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.UpsertExample(model.Example{
		NodeId: node.Id, Name: "scripted", Status: 200,
		MockScript: `respond({ status: 201, body: "ok" });`,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.ListExamples(node.Id)
	if err != nil || len(got) != 1 {
		t.Fatalf("list = %v, %v", got, err)
	}
	if got[0].MockScript != saved.MockScript {
		t.Fatalf("mockScript = %q, want %q", got[0].MockScript, saved.MockScript)
	}
}
