package reference_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"apistock.dev/modules/openapi/reference"
)

func TestPostman(t *testing.T) {
	out, err := reference.Postman([]byte(shopSpec), reference.ExportOptions{BaseURL: "https://shop.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	again, _ := reference.Postman([]byte(shopSpec), reference.ExportOptions{BaseURL: "https://shop.example.com"})
	if !bytes.Equal(out, again) {
		t.Error("Postman() isn't deterministic")
	}

	var c struct {
		Info     struct{ Name, Schema string }
		Variable []struct{ Key, Value string }
		Item     []struct {
			Name string
			Item []struct {
				Name    string
				Request struct {
					Method string
					Auth   struct {
						Type   string
						Bearer []struct{ Key, Value string }
					}
					Header []struct{ Key, Value string }
					URL    struct {
						Raw      string
						Host     []string
						Path     []string
						Variable []struct{ Key, Value string }
					} `json:"url"`
					Body *struct {
						Mode, Raw string
					}
				}
			}
		}
	}
	if err := json.Unmarshal(out, &c); err != nil {
		t.Fatalf("Postman() isn't JSON: %v\n%s", err, out)
	}
	if c.Info.Name != "Shop API" || !strings.Contains(c.Info.Schema, "v2.1.0") || len(c.Variable) != 2 || c.Variable[0].Value != "https://shop.example.com" || c.Variable[1].Key != "token" {
		t.Errorf("collection info and variables = %+v %+v", c.Info, c.Variable)
	}
	if len(c.Item) != 1 || c.Item[0].Name != "Orders" || len(c.Item[0].Item) != 2 {
		t.Fatalf("folders = %+v, want Orders with 2 requests", c.Item)
	}
	list, create := c.Item[0].Item[0].Request, c.Item[0].Item[1].Request
	if list.Method != "GET" || list.Auth.Type != "noauth" || list.Body != nil {
		t.Errorf("list request = %+v, want GET without auth or body", list)
	}
	if create.Method != "POST" || create.Auth.Type != "bearer" || len(create.Auth.Bearer) != 1 || create.Auth.Bearer[0].Value != "{{token}}" {
		t.Errorf("create auth = %+v, want bearer {{token}}", create.Auth)
	}
	if create.URL.Raw != "{{baseUrl}}/v1/shops/:shopId/orders" || strings.Join(create.URL.Path, "/") != "v1/shops/:shopId/orders" || len(create.URL.Variable) != 1 || create.URL.Variable[0].Key != "shopId" {
		t.Errorf("create URL = %+v", create.URL)
	}
	if create.Body == nil || create.Body.Mode != "raw" || !strings.Contains(create.Body.Raw, `"id": "ord_1"`) || len(create.Header) != 1 || create.Header[0].Value != "application/json" {
		t.Errorf("create body = %+v, headers %+v; want an example JSON body", create.Body, create.Header)
	}

	if _, err := reference.Postman([]byte("not json"), reference.ExportOptions{}); err == nil {
		t.Error("Postman(invalid document) error = nil")
	}
}

func TestLLMs(t *testing.T) {
	out, err := reference.LLMs([]byte(shopSpec), reference.ExportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	again, _ := reference.LLMs([]byte(shopSpec), reference.ExportOptions{})
	if !bytes.Equal(out, again) {
		t.Error("LLMs() isn't deterministic")
	}
	text := string(out)
	for _, want := range []string{
		"# Shop API\n\n> Sells things.",
		"## Authentication\n\n- `bearer` (bearer): Token from POST /login.\n",
		"## Orders\n\n- [List orders](/docs/orders/list-orders.md): `GET /v1/shops/{shopId}/orders`\n- [Create an order](/docs/orders/create-order.md): `POST /v1/shops/{shopId}/orders`\n",
		"## Optional\n\n- [OpenAPI document](/openapi.json)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("llms.txt lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "<script>") {
		t.Errorf("llms.txt keeps HTML from the description:\n%s", text)
	}
	if other, _ := reference.LLMs([]byte(shopSpec), reference.ExportOptions{DocsPath: "/reference"}); !strings.Contains(string(other), "(/reference/orders/create-order.md)") {
		t.Errorf("llms.txt with DocsPath /reference:\n%s", other)
	}
}
