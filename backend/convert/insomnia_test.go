package convert

import (
	"strings"
	"testing"
)

const sampleInsomnia = `{
  "__export_format": 4,
  "resources": [
    {"_type": "workspace", "_id": "wrk_1", "name": "My APIs"},
    {"_type": "environment", "_id": "env_1", "parentId": "wrk_1",
     "data": {"base": "https://api.x.io", "ver": 2}},
    {"_type": "request_group", "_id": "fld_1", "parentId": "wrk_1", "name": "Users", "metaSortKey": 10},
    {"_type": "request", "_id": "req_1", "parentId": "fld_1", "name": "List",
     "method": "GET", "url": "{{ _.base }}/users",
     "headers": [{"name": "Accept", "value": "application/json"}],
     "parameters": [{"name": "page", "value": "1", "disabled": true}],
     "metaSortKey": 20},
    {"_type": "request", "_id": "req_2", "parentId": "fld_1", "name": "Create",
     "method": "POST", "url": "{{ _.base }}/users",
     "body": {"mimeType": "application/json", "text": "{\"name\": \"{{ _.ver }}\"}"},
     "authentication": {"type": "bearer", "token": "tok1"},
     "metaSortKey": 30}
  ]
}`

func TestInsomniaImport(t *testing.T) {
	res, err := Import("insomnia", sampleInsomnia)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Collection.Name != "My APIs" {
		t.Errorf("name = %s", res.Collection.Name)
	}
	// base environment → 集合变量
	vars := map[string]string{}
	for _, v := range res.Collection.Variables {
		vars[v.Key] = v.Value
	}
	if vars["base"] != "https://api.x.io" || vars["ver"] != "2" {
		t.Errorf("vars = %v", vars)
	}

	if len(res.Children) != 3 { // folder + 2 requests
		t.Fatalf("children = %d", len(res.Children))
	}
	folder := res.Children[0]
	if folder.Kind != "folder" || folder.Name != "Users" {
		t.Errorf("folder = %+v", folder)
	}
	for _, n := range res.Children[1:] {
		if n.ParentId != folder.Id {
			t.Errorf("request %s parent = %s, want %s", n.Name, n.ParentId, folder.Id)
		}
	}
	list := res.Children[1]
	// {{ _.base }} → {{base}}
	if list.Request.Url != "{{base}}/users" {
		t.Errorf("url = %s", list.Request.Url)
	}
	if len(list.Request.Params) != 1 || list.Request.Params[0].Enabled {
		t.Errorf("params = %+v (disabled should carry over)", list.Request.Params)
	}
	create := res.Children[2]
	if create.Request.Body.Kind != "raw" || !strings.Contains(create.Request.Body.Text, "{{ver}}") {
		t.Errorf("body = %+v", create.Request.Body)
	}
	if create.Request.Auth.Type != "bearer" || create.Request.Auth.Params["token"] != "tok1" {
		t.Errorf("auth = %+v", create.Request.Auth)
	}
}

func TestInsomniaAutoDetect(t *testing.T) {
	if _, err := Import("auto", sampleInsomnia); err != nil {
		t.Errorf("auto insomnia: %v", err)
	}
}
