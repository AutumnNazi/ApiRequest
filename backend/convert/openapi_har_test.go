package convert

import (
	"strings"
	"testing"
)

const sampleOpenAPI = `{
  "openapi": "3.0.1",
  "info": {"title": "Pet API"},
  "servers": [{"url": "https://api.pets.io/v2"}],
  "paths": {
    "/pets": {
      "get": {
        "operationId": "listPets",
        "tags": ["pets"],
        "parameters": [
          {"name": "limit", "in": "query", "required": true},
          {"name": "X-Tenant", "in": "header", "required": false}
        ]
      },
      "post": {
        "operationId": "createPet",
        "tags": ["pets"],
        "requestBody": {
          "content": {
            "application/json": {"example": {"name": "kitty", "age": 2}}
          }
        }
      }
    },
    "/pets/{petId}": {
      "get": {"summary": "Get one pet", "tags": ["pets"]}
    },
    "/health": {
      "get": {"operationId": "health"}
    }
  }
}`

func TestOpenAPIImport(t *testing.T) {
	res, err := Import("openapi", sampleOpenAPI)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.Collection.Name != "Pet API" {
		t.Errorf("name = %s", res.Collection.Name)
	}
	// baseUrl 变量
	if len(res.Collection.Variables) != 1 || res.Collection.Variables[0].Value != "https://api.pets.io/v2" {
		t.Errorf("vars = %+v", res.Collection.Variables)
	}

	var folders, requests []string
	byId := map[string]string{} // id → name
	for _, n := range res.Children {
		byId[n.Id] = n.Name
		if n.Kind == "folder" {
			folders = append(folders, n.Name)
		} else {
			requests = append(requests, n.Name)
		}
	}
	// pets 一个 folder；health 无 tag 挂根
	if len(folders) != 1 || folders[0] != "pets" {
		t.Errorf("folders = %v", folders)
	}
	if len(requests) != 4 {
		t.Errorf("requests = %v", requests)
	}

	for _, n := range res.Children {
		if n.Kind != "request" {
			continue
		}
		switch n.Name {
		case "listPets":
			if n.Request.Url != "{{baseUrl}}/pets" {
				t.Errorf("listPets url = %s", n.Request.Url)
			}
			if len(n.Request.Params) != 1 || n.Request.Params[0].Key != "limit" || !n.Request.Params[0].Enabled {
				t.Errorf("listPets params = %+v", n.Request.Params)
			}
			if len(n.Request.Headers) != 1 || n.Request.Headers[0].Enabled {
				t.Errorf("listPets headers = %+v (optional → disabled)", n.Request.Headers)
			}
		case "createPet":
			if n.Request.Body.Kind != "raw" || !strings.Contains(n.Request.Body.Text, "kitty") {
				t.Errorf("createPet body = %+v", n.Request.Body)
			}
		case "Get one pet":
			if n.Request.Url != "{{baseUrl}}/pets/{{petId}}" {
				t.Errorf("path param url = %s", n.Request.Url)
			}
		}
	}
}

func TestOpenAPIYamlImport(t *testing.T) {
	yamlDoc := `
openapi: "3.0.0"
info:
  title: Yaml API
servers:
  - url: https://y.io
paths:
  /items:
    get:
      operationId: listItems
`
	res, err := Import("openapi", yamlDoc)
	if err != nil {
		t.Fatalf("yaml import: %v", err)
	}
	if res.Collection.Name != "Yaml API" || len(res.Children) != 1 {
		t.Errorf("res = %+v", res)
	}
}

func TestSwagger2Import(t *testing.T) {
	doc := `{
	  "swagger": "2.0",
	  "info": {"title": "Old API"},
	  "host": "old.io", "basePath": "/v1", "schemes": ["https"],
	  "paths": {"/a": {"get": {"operationId": "getA"}}}
	}`
	res, err := Import("openapi", doc)
	if err != nil {
		t.Fatal(err)
	}
	if res.Collection.Variables[0].Value != "https://old.io/v1" {
		t.Errorf("baseUrl = %s", res.Collection.Variables[0].Value)
	}
}

const sampleHAR = `{
  "log": {
    "entries": [
      {
        "request": {
          "method": "GET",
          "url": "https://api.a.io/users?page=1",
          "headers": [
            {"name": "Accept", "value": "application/json"},
            {"name": "Cookie", "value": "session=x"},
            {"name": ":authority", "value": "api.a.io"}
          ],
          "queryString": [{"name": "page", "value": "1"}]
        }
      },
      {
        "request": {
          "method": "POST",
          "url": "https://api.a.io/users",
          "headers": [{"name": "Content-Type", "value": "application/json"}],
          "queryString": [],
          "postData": {"mimeType": "application/json", "text": "{\"n\":1}"}
        }
      },
      {
        "request": {
          "method": "GET",
          "url": "https://cdn.b.io/logo.png",
          "headers": [],
          "queryString": []
        }
      }
    ]
  }
}`

func TestHARImport(t *testing.T) {
	res, err := Import("har", sampleHAR)
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	var folders []string
	requestCount := 0
	for _, n := range res.Children {
		if n.Kind == "folder" {
			folders = append(folders, n.Name)
		} else {
			requestCount++
		}
	}
	// 两个 host → 两个 folder
	if len(folders) != 2 || requestCount != 3 {
		t.Errorf("folders = %v, requests = %d", folders, requestCount)
	}
	// 第一个请求：Cookie 与伪头应被剥离
	for _, n := range res.Children {
		if n.Kind == "request" && strings.Contains(n.Name, "/users") && n.Request.Method == "GET" {
			for _, h := range n.Request.Headers {
				if strings.EqualFold(h.Key, "Cookie") || strings.HasPrefix(h.Key, ":") {
					t.Errorf("header %s should be stripped", h.Key)
				}
			}
			if len(n.Request.Params) != 1 || n.Request.Params[0].Key != "page" {
				t.Errorf("params = %+v", n.Request.Params)
			}
		}
		if n.Kind == "request" && n.Request.Method == "POST" {
			if n.Request.Body.Kind != "raw" || n.Request.Body.Text != `{"n":1}` {
				t.Errorf("post body = %+v", n.Request.Body)
			}
		}
	}
}

func TestAutoDetectNewFormats(t *testing.T) {
	if _, err := Import("auto", sampleOpenAPI); err != nil {
		t.Errorf("auto openapi: %v", err)
	}
	if _, err := Import("auto", sampleHAR); err != nil {
		t.Errorf("auto har: %v", err)
	}
}
