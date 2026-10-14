package binding

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"apirequest/backend/convert"
	"apirequest/backend/model"
	"apirequest/backend/secrets"
	"apirequest/backend/storage"
)

func TestExportDataRedactsStoredSecrets(t *testing.T) {
	dir := t.TempDir()
	vault := secrets.NewWithKeyring(dir, &bindingMemoryKeyring{})
	store, err := storage.OpenWithVault(dir, vault)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	workspace, _ := store.EnsureDefaultWorkspace()
	collection, err := store.UpsertNode(model.Node{
		WorkspaceId: workspace.Id,
		Kind:        "collection",
		Name:        "secured",
		Auth: &model.Auth{Type: "bearer", Params: map[string]string{
			"token": "collection-token",
		}},
		Variables: []model.Variable{{
			Key: "apiToken", Value: "collection-variable-secret", Type: "secret", Enabled: true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.UpsertNode(model.Node{
		WorkspaceId: workspace.Id,
		ParentId:    collection.Id,
		Kind:        "request",
		Name:        "private",
		Request: &model.HttpRequest{
			Method: "GET",
			Url:    "https://example.test/private",
			Auth: model.Auth{Type: "basic", Params: map[string]string{
				"username": "alice",
				"password": "request-password",
			}},
			Settings: model.DefaultSettings(),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	api := NewConvertApi(store)
	for _, format := range []string{"postman", "curl"} {
		out, exportErr := api.ExportData(collection.Id, format)
		if exportErr != nil {
			t.Fatalf("export %s: %v", format, exportErr)
		}
		for _, secret := range []string{"collection-token", "collection-variable-secret", "request-password"} {
			if strings.Contains(out, secret) {
				t.Fatalf("%s export contains %q:\n%s", format, secret, out)
			}
		}
		if !strings.Contains(out, "redacted") {
			t.Fatalf("%s export has no redaction marker:\n%s", format, out)
		}
	}
}

func TestImportCommitRollsBackWholeTreeOnInvalidChild(t *testing.T) {
	dir := t.TempDir()
	keyring := &bindingMemoryKeyring{}
	store, err := storage.OpenWithVault(dir, secrets.NewWithKeyring(dir, keyring))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	workspace, _ := store.EnsureDefaultWorkspace()
	db, err := sql.Open("sqlite", filepath.Join(dir, "apirequest.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TRIGGER fail_import_child
		BEFORE INSERT ON node WHEN NEW.name = 'fail-me'
		BEGIN SELECT RAISE(FAIL, 'injected import failure'); END`); err != nil {
		t.Fatal(err)
	}

	result := convert.ImportResult{
		Collection: model.Node{Id: "root", Kind: "collection", Name: "partial"},
		Children: []model.Node{
			{Id: "request", ParentId: "root", Kind: "request", Name: "first", Request: &model.HttpRequest{
				Method: "GET", Url: "https://example.test", Settings: model.DefaultSettings(),
				Auth: model.Auth{Type: "bearer", Params: map[string]string{"token": "rolled-back-secret"}},
			}},
			{Id: "folder", ParentId: "root", Kind: "folder", Name: "fail-me"},
		},
	}
	if _, err := NewConvertApi(store).ImportCommit(workspace.Id, result); err == nil {
		t.Fatal("invalid import tree was accepted")
	}
	nodes, err := store.ListNodes(workspace.Id)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 0 {
		t.Fatalf("failed import left %d nodes behind: %+v", len(nodes), nodes)
	}
	if len(keyring.values) != 0 {
		t.Fatalf("failed import left Vault values behind: %+v", keyring.values)
	}
}

// ImportCommit 落库导入器提取的响应示例（占位 requestId 经映射换算）：
// Mock Server 以 Example 为数据源，导入 OpenAPI 即具备 Mock 响应能力。
func TestImportCommitPersistsImportedExamples(t *testing.T) {
	dir := t.TempDir()
	store, err := storage.OpenWithVault(dir, secrets.NewWithKeyring(dir, &bindingMemoryKeyring{}))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	workspace, _ := store.EnsureDefaultWorkspace()

	result := convert.ImportResult{
		Collection: model.Node{Id: "root", Kind: "collection", Name: "with-examples"},
		Children: []model.Node{
			{Id: "req-1", ParentId: "root", Kind: "request", Name: "get user", Request: &model.HttpRequest{
				Method: "GET", Url: "https://example.test/u", Settings: model.DefaultSettings(),
			}},
		},
		Examples: []convert.ImportedExample{
			{RequestId: "req-1", Name: "200 响应示例", Status: 200, Body: `{"name":"alice"}`},
		},
	}
	out, err := NewConvertApi(store).ImportCommit(workspace.Id, result)
	if err != nil {
		t.Fatalf("import commit: %v", err)
	}
	if out.Collection.Id == "" {
		t.Fatal("collection not saved")
	}
	// 示例挂在 GET 请求的真实新 id 上
	nodes, _ := store.ListNodes(workspace.Id)
	var reqId string
	for _, n := range nodes {
		if n.Kind == "request" {
			reqId = n.Id
		}
	}
	examples, err := store.ListExamples(reqId)
	if err != nil {
		t.Fatal(err)
	}
	if len(examples) != 1 {
		t.Fatalf("examples = %d, want 1", len(examples))
	}
	if examples[0].Name != "200 响应示例" || examples[0].Status != 200 {
		t.Fatalf("example = %+v", examples[0])
	}
	if !strings.Contains(examples[0].Body, "alice") {
		t.Fatalf("example body = %q", examples[0].Body)
	}
}
