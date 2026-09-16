package openapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"gorbital.dev/modules/openapi"
)

// baselineDoc is a small OpenAPI document: a list endpoint with a query
// parameter and a paginated response, and an update endpoint with a body.
const baselineDoc = `{
  "openapi": "3.1.0",
  "paths": {
    "/ops/settings": {
      "get": {
        "parameters": [
          {"in": "query", "name": "group", "schema": {"type": "string"}}
        ],
        "responses": {
          "200": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/SettingList"}}}},
          "401": {"content": {"application/problem+json": {"schema": {"$ref": "#/components/schemas/Problem"}}}}
        },
        "security": [{"bearer": []}]
      }
    },
    "/ops/settings/{key}": {
      "put": {
        "parameters": [
          {"in": "path", "name": "key", "required": true, "schema": {"type": "string"}}
        ],
        "requestBody": {
          "required": true,
          "content": {"application/json": {"schema": {"$ref": "#/components/schemas/UpdateBody"}}}
        },
        "responses": {
          "200": {"content": {"application/json": {"schema": {"$ref": "#/components/schemas/Setting"}}}}
        }
      }
    },
    "/v1/ping": {
      "get": {"responses": {"200": {"content": {"application/json": {"schema": {"type": "object"}}}}}}
    }
  },
  "components": {
    "schemas": {
      "Problem": {
        "type": "object",
        "required": ["code", "status"],
        "properties": {"code": {"type": "string"}, "status": {"type": "integer"}}
      },
      "Setting": {
        "type": "object",
        "required": ["key", "value", "history"],
        "properties": {
          "key": {"type": "string"},
          "value": {},
          "reason": {"type": ["string", "null"]},
          "history": {"type": "array", "items": {"$ref": "#/components/schemas/Setting"}},
          "labels": {"type": "object", "additionalProperties": {"type": "string"}}
        }
      },
      "SettingList": {
        "type": "object",
        "required": ["items"],
        "properties": {"items": {"type": "array", "items": {"$ref": "#/components/schemas/Setting"}}}
      },
      "UpdateBody": {
        "type": "object",
        "required": ["value"],
        "properties": {
          "value": {},
          "reason": {"type": "string"},
          "mode": {"type": "string", "enum": ["set", "merge"]}
        }
      }
    }
  }
}`

func TestCheckCompatible(t *testing.T) {
	tests := []struct {
		name   string
		change func(doc map[string]any)
		want   []string
	}{
		{"unchanged", func(map[string]any) {}, nil},
		{
			"additions are compatible",
			func(doc map[string]any) {
				paths(doc)["/ops/jobs"] = map[string]any{"get": map[string]any{}}
				op(doc, "/ops/settings", "get")["parameters"] = []any{
					map[string]any{"in": "query", "name": "group", "schema": map[string]any{"type": "string"}},
					map[string]any{"in": "query", "name": "cursor", "schema": map[string]any{"type": "string"}},
				}
				schema(doc, "Setting")["properties"].(map[string]any)["updated_at"] = map[string]any{"type": "string"}
				schema(doc, "UpdateBody")["properties"].(map[string]any)["note"] = map[string]any{"type": "string"}
				responses(doc, "/ops/settings", "get")["403"] = map[string]any{}
				schema(doc, "UpdateBody")["properties"].(map[string]any)["mode"].(map[string]any)["enum"] = []any{"set", "merge", "replace"}
			},
			nil,
		},
		{
			"paths outside the prefix aren't checked",
			func(doc map[string]any) { delete(paths(doc), "/v1/ping") },
			nil,
		},
		{
			"renamed path parameter",
			func(doc map[string]any) {
				paths(doc)["/ops/settings/{name}"] = paths(doc)["/ops/settings/{key}"]
				delete(paths(doc), "/ops/settings/{key}")
			},
			nil,
		},
		{
			"removed path",
			func(doc map[string]any) { delete(paths(doc), "/ops/settings/{key}") },
			[]string{"PUT /ops/settings/{key}: removed"},
		},
		{
			"removed method",
			func(doc map[string]any) {
				item := paths(doc)["/ops/settings/{key}"].(map[string]any)
				item["patch"] = item["put"]
				delete(item, "put")
			},
			[]string{"PUT /ops/settings/{key}: removed"},
		},
		{
			"removed and required parameters",
			func(doc map[string]any) {
				op(doc, "/ops/settings", "get")["parameters"] = []any{
					map[string]any{"in": "query", "name": "cursor", "required": true, "schema": map[string]any{"type": "string"}},
				}
			},
			[]string{
				"GET /ops/settings: parameter query cursor: added as required",
				"GET /ops/settings: parameter query group: removed",
			},
		},
		{
			"parameter type narrowed",
			func(doc map[string]any) {
				op(doc, "/ops/settings", "get")["parameters"].([]any)[0].(map[string]any)["schema"] = map[string]any{"type": "integer"}
			},
			[]string{"GET /ops/settings: parameter query group: type changed from string to integer"},
		},
		{
			"removed response status and property",
			func(doc map[string]any) {
				delete(responses(doc, "/ops/settings", "get"), "401")
				delete(schema(doc, "Setting")["properties"].(map[string]any), "reason")
			},
			[]string{
				"GET /ops/settings: response 200 application/json: items[].reason: removed",
				"GET /ops/settings: response 401: removed",
				"PUT /ops/settings/{key}: response 200 application/json: reason: removed",
			},
		},
		{
			"response type widened and property optional",
			func(doc map[string]any) {
				props := schema(doc, "Setting")["properties"].(map[string]any)
				props["key"] = map[string]any{"type": []any{"string", "null"}}
				schema(doc, "Setting")["required"] = []any{"value", "history"}
			},
			[]string{
				"GET /ops/settings: response 200 application/json: items[].key: no longer always present",
				"GET /ops/settings: response 200 application/json: items[].key: type changed from string to string or null",
				"PUT /ops/settings/{key}: response 200 application/json: key: no longer always present",
				"PUT /ops/settings/{key}: response 200 application/json: key: type changed from string to string or null",
			},
		},
		{
			"response type narrowed is compatible, map values checked, recursive schemas once",
			func(doc map[string]any) {
				schema(doc, "Setting")["properties"].(map[string]any)["reason"] = map[string]any{"type": "string"}
				schema(doc, "Setting")["properties"].(map[string]any)["labels"].(map[string]any)["additionalProperties"] = map[string]any{"type": "integer"}
			},
			[]string{
				"GET /ops/settings: response 200 application/json: items[].labels{}: type changed from string to integer",
				"PUT /ops/settings/{key}: response 200 application/json: labels{}: type changed from string to integer",
			},
		},
		{
			"request body changes",
			func(doc map[string]any) {
				body := schema(doc, "UpdateBody")
				body["required"] = []any{"value", "reason", "ticket"}
				body["properties"].(map[string]any)["ticket"] = map[string]any{"type": "string"}
				body["properties"].(map[string]any)["mode"].(map[string]any)["enum"] = []any{"set"}
			},
			[]string{
				"PUT /ops/settings/{key}: request application/json: mode: allowed values changed from [\"set\",\"merge\"] to [\"set\"]",
				"PUT /ops/settings/{key}: request application/json: reason: now required",
				"PUT /ops/settings/{key}: request application/json: ticket: added as required",
			},
		},
		{
			"request property removed, request type widened is compatible",
			func(doc map[string]any) {
				props := schema(doc, "UpdateBody")["properties"].(map[string]any)
				delete(props, "mode")
				props["reason"] = map[string]any{"type": []any{"string", "null"}}
			},
			[]string{"PUT /ops/settings/{key}: request application/json: mode: removed"},
		},
		{
			"request body removed",
			func(doc map[string]any) { delete(op(doc, "/ops/settings/{key}", "put"), "requestBody") },
			[]string{"PUT /ops/settings/{key}: request body: removed"},
		},
		{
			"media type removed",
			func(doc map[string]any) {
				content := op(doc, "/ops/settings/{key}", "put")["requestBody"].(map[string]any)["content"].(map[string]any)
				content["application/merge-patch+json"] = content["application/json"]
				delete(content, "application/json")
			},
			[]string{"PUT /ops/settings/{key}: request application/json: removed"},
		},
		{
			"authentication added",
			func(doc map[string]any) {
				op(doc, "/ops/settings/{key}", "put")["security"] = []any{map[string]any{"bearer": []any{}}}
			},
			[]string{"PUT /ops/settings/{key}: now requires authentication"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var doc map[string]any
			if err := json.Unmarshal([]byte(baselineDoc), &doc); err != nil {
				t.Fatal(err)
			}
			tt.change(doc)
			current, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			found, err := openapi.CheckCompatible([]byte(baselineDoc), current, "/ops/")
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, f := range found {
				got = append(got, f.String())
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("CheckCompatible() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestCheckCompatibleRejectsInvalidDocuments(t *testing.T) {
	if _, err := openapi.CheckCompatible([]byte("{"), []byte(baselineDoc), "/"); err == nil {
		t.Error("an invalid baseline was accepted")
	}
	if _, err := openapi.CheckCompatible([]byte(baselineDoc), []byte("nope"), "/"); err == nil {
		t.Error("an invalid current document was accepted")
	}
}

// TestCheckCompatibleWithHuma checks documents Huma generates: removing a
// response field from a Go struct is reported.
func TestCheckCompatibleWithHuma(t *testing.T) {
	type before struct {
		Body struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
	}
	type after struct {
		Body struct {
			Key   string `json:"key"`
			Value string `json:"value,omitempty"`
			Note  string `json:"note"`
		}
	}
	spec := func(register func(huma.API)) []byte {
		api := openapi.New(http.NewServeMux(), "Test", "1.0.0")
		register(api)
		var b bytes.Buffer
		if err := openapi.WriteSpec(&b, api); err != nil {
			t.Fatal(err)
		}
		return b.Bytes()
	}
	operation := huma.Operation{OperationID: "get-setting", Method: http.MethodGet, Path: "/ops/settings/{key}"}
	type input struct {
		Key string `path:"key"`
	}
	old := spec(func(api huma.API) {
		huma.Register(api, operation, func(context.Context, *input) (*before, error) { return nil, nil })
	})
	current := spec(func(api huma.API) {
		huma.Register(api, operation, func(context.Context, *input) (*after, error) { return nil, nil })
	})

	found, err := openapi.CheckCompatible(old, current, "/ops/")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].String() != "GET /ops/settings/{key}: response 200 application/json: value: no longer always present" {
		t.Errorf("CheckCompatible() = %v", found)
	}
}

func paths(doc map[string]any) map[string]any { return doc["paths"].(map[string]any) }

func op(doc map[string]any, path, method string) map[string]any {
	return paths(doc)[path].(map[string]any)[method].(map[string]any)
}

func responses(doc map[string]any, path, method string) map[string]any {
	return op(doc, path, method)["responses"].(map[string]any)
}

func schema(doc map[string]any, name string) map[string]any {
	return doc["components"].(map[string]any)["schemas"].(map[string]any)[name].(map[string]any)
}
