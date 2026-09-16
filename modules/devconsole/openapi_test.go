package devconsole_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"gorbital.dev/modules/devconsole"
)

// fullSources returns a value for every section, with every optional field
// filled.
func fullSources() devconsole.Sources {
	now := time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC)
	pct := 25
	return devconsole.Sources{
		App: func(context.Context) (devconsole.App, error) {
			return devconsole.App{
				Name: "acme-api", Version: "v1.0.0", Commit: "abc", GoVersion: "go1.26.0", Env: "development",
				Libraries: []devconsole.Library{{Path: "gorbital.dev", Version: "(devel)", Replaced: true}},
				Modules:   []string{"Projects"},
				Jobs:      []devconsole.Job{{Name: "retention", Description: "d", Enabled: true, Schedule: "@daily", Modified: true, NextRunAt: &now}},
				Settings: []devconsole.Setting{{Key: "example.ping_message", Group: "example", Description: "d", Kind: "string",
					Value: json.RawMessage(`"pong"`), Default: json.RawMessage(`"pong"`), OrgOverridable: true}},
				Flags: []devconsole.Flag{{Key: "example.ping_time", Group: "example", Description: "d", Client: true, Enabled: true, Percentage: &pct, Targets: 2}},
				Permissions: []devconsole.PermissionCatalog{{Name: "platform",
					Permissions: []devconsole.Permission{{Name: "ops.audit.read", Description: "d"}},
					Roles:       []devconsole.Role{{Name: "ops_viewer", Description: "d", Permissions: []string{"ops.audit.read"}}}}},
			}, nil
		},
		Routes: func(context.Context) ([]devconsole.Route, error) {
			return []devconsole.Route{{Method: "GET", Path: "/v1/ping", OperationID: "ping", Summary: "Ping", Tags: []string{"Ping"}, Secured: true, Source: devconsole.RouteOpenAPI}}, nil
		},
		Config: func(context.Context) ([]devconsole.EnvKey, error) {
			return []devconsole.EnvKey{{Name: "APP_ADDR", Set: true, Value: "127.0.0.1:8080"}}, nil
		},
		Mail: func(context.Context) (devconsole.Mail, error) {
			return devconsole.Mail{WebURL: "http://127.0.0.1:8025", Total: 1, Messages: []devconsole.Message{{
				ID: "m", From: devconsole.Address{Name: "A", Address: "a@x.test"}, To: []devconsole.Address{{Address: "b@x.test"}},
				Subject: "s", Snippet: "n", Created: now, Size: 1, Attachments: 1, Read: true,
			}}}, nil
		},
		Migrations: func(context.Context) (devconsole.Migrations, error) {
			return devconsole.Migrations{Current: 2, Latest: 3, Pending: 1}, nil
		},
		Jobs: func(context.Context) ([]devconsole.JobRun, error) {
			return []devconsole.JobRun{{ID: 1, Kind: "retention", Queue: "default", State: "completed", Attempt: 1, MaxAttempts: 3,
				CreatedAt: now, ScheduledAt: now, AttemptedAt: &now, FinalizedAt: &now, Errors: []string{"e"}, RequestID: "req"}}, nil
		},
	}
}

// schemaChecker checks JSON values against the console's OpenAPI schemas:
// every property is declared, every declared one is present (the values
// fill every field), and so is every required one.
type schemaChecker struct {
	t       *testing.T
	schemas map[string]map[string]any
}

func (c schemaChecker) check(where string, schema map[string]any, v any) {
	if ref, ok := schema["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, "#/components/schemas/")
		s, ok := c.schemas[name]
		if !ok {
			c.t.Errorf("%s: unknown schema %s", where, ref)
			return
		}
		schema = s
	}
	switch schema["type"] {
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			c.t.Errorf("%s: %v isn't an object", where, v)
			return
		}
		props, _ := schema["properties"].(map[string]any)
		if props == nil {
			return // free-form
		}
		for key, value := range obj {
			p, ok := props[key].(map[string]any)
			if !ok {
				c.t.Errorf("%s: property %q isn't in the OpenAPI document", where, key)
				continue
			}
			c.check(where+"."+key, p, value)
		}
		for key := range props {
			if _, ok := obj[key]; !ok {
				c.t.Errorf("%s: property %q is documented but wasn't served (every field is filled)", where, key)
			}
		}
		required, _ := schema["required"].([]any)
		for _, r := range required {
			if _, ok := obj[r.(string)]; !ok {
				c.t.Errorf("%s: required property %q missing", where, r)
			}
		}
	case "array":
		list, ok := v.([]any)
		if !ok {
			c.t.Errorf("%s: %v isn't an array", where, v)
			return
		}
		items, _ := schema["items"].(map[string]any)
		for i, item := range list {
			c.check(where+"[]", items, item)
			if i == 0 {
				break
			}
		}
	}
}

// TestOpenAPIDescribesEveryEndpoint serves every section with every field
// filled and checks each response against /_dev/openapi.json, and that the
// document lists exactly the served paths.
func TestOpenAPIDescribesEveryEndpoint(t *testing.T) {
	logs, _ := devconsole.NewLogs(10)
	base, c := newServer(t, appHandler(), devconsole.WithLogs(logs), devconsole.WithSources(fullSources()))
	c.RecordRequest(devconsole.Request{Time: time.Now(), Method: "GET", Route: "/v1/ping", Path: "/v1/ping", Status: 200, DurationMS: 1.5, RequestID: "r", TraceID: "t"})
	attrs := make([]any, 0, 102)
	for i := range 51 { // one over the limit, so dropped_attrs is set
		attrs = append(attrs, fmt.Sprintf("k%d", i), "v")
	}
	slog.New(logs.Handler()).Info("hello", attrs...)

	r := get(t, base, "/_dev/openapi.json", "", true)
	var doc struct {
		Paths      map[string]map[string]map[string]any `json:"paths"`
		Components struct {
			Schemas map[string]map[string]any `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal([]byte(r.body), &doc); err != nil || r.code != 200 {
		t.Fatalf("openapi.json = %d %v", r.code, err)
	}
	var index devconsole.Index
	_ = json.Unmarshal([]byte(get(t, base, "/_dev/", "", true).body), &index)
	var documented []string
	for path := range doc.Paths {
		documented = append(documented, path)
	}
	slices.Sort(documented)
	if strings.Join(documented, ",") != strings.Join(index.Endpoints, ",") {
		t.Errorf("documented paths %v, served %v", documented, index.Endpoints)
	}

	checker := schemaChecker{t: t, schemas: doc.Components.Schemas}
	for _, path := range index.Endpoints {
		if strings.HasSuffix(path, "/stream") || strings.HasSuffix(path, "openapi.json") {
			continue
		}
		res := get(t, base, path, "", true)
		if res.code != http.StatusOK {
			t.Errorf("%s = %d %s", path, res.code, res.body)
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(res.body), &v); err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		op := doc.Paths[path]["get"]
		schema := op["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
		checker.check(path, schema, v)
	}
	// Stream events and problems.
	for name, v := range map[string]any{"StreamEnd": devconsole.StreamEnd{Reason: "shutdown"}, "StreamDropped": devconsole.StreamDropped{Count: 1}} {
		data, _ := json.Marshal(v)
		var decoded any
		_ = json.Unmarshal(data, &decoded)
		checker.check(name, map[string]any{"$ref": "#/components/schemas/" + name}, decoded)
	}
}
