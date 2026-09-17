package app_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"example.com/acme-api/internal/app"
)

func TestOpsSystem(t *testing.T) {
	a := newApp(t, nil)
	h := a.Handler()
	if r := do(t, h, "GET", "/ops/system", ""); r.code != http.StatusUnauthorized {
		t.Errorf("GET /ops/system without a session = %d, want 401", r.code)
	}
	viewer, _ := signIn(t, a, "viewer@example.com", "ops_viewer")
	r := do(t, h, "GET", "/ops/system", "", viewer...)
	if r.code != http.StatusOK {
		t.Fatalf("GET /ops/system as ops_viewer = %d %s", r.code, r.body)
	}

	var body struct {
		Instance struct {
			ID            string `json:"id"`
			UptimeSeconds int64  `json:"uptime_seconds"`
		} `json:"instance"`
		Checks []struct {
			Name, Status string
		} `json:"checks"`
		Database struct {
			Status string `json:"status"`
			Pool   struct {
				Max int32 `json:"max"`
			} `json:"pool"`
			Migrations struct {
				Current, Latest int64
				Pending         int
			} `json:"migrations"`
		} `json:"database"`
		Runtime struct {
			GoVersion  string `json:"go_version"`
			Goroutines int    `json:"goroutines"`
		} `json:"runtime"`
	}
	if err := json.Unmarshal([]byte(r.body), &body); err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(body.Instance.ID) {
		t.Errorf("instance.id = %q", body.Instance.ID)
	}
	if len(body.Checks) == 0 || body.Checks[0].Name != "postgres" || body.Checks[0].Status != "ok" {
		t.Errorf("checks = %+v, want postgres ok", body.Checks)
	}
	m := body.Database.Migrations
	if body.Database.Status != "ok" || body.Database.Pool.Max <= 0 || m.Latest == 0 || m.Current != m.Latest || m.Pending != 0 {
		t.Errorf("database = %+v, want ok, a pool and every migration applied", body.Database)
	}
	if !strings.HasPrefix(body.Runtime.GoVersion, "go") || body.Runtime.Goroutines == 0 {
		t.Errorf("runtime = %+v", body.Runtime)
	}
	for _, secret := range []string{"postgres://", "password", "DATABASE_URL"} {
		if strings.Contains(r.body, secret) {
			t.Errorf("GET /ops/system response contains %q", secret)
		}
	}
}

// TestOpsOperationsDeclareSecurity checks the contract every /ops endpoint
// promises: a bearer session, 401 without one and 403 without the
// permission or two-factor authentication (ADR-0026, ADR-0051). The
// use-case test TestEveryOperationAuthorizesFirst checks the behaviour.
func TestOpsOperationsDeclareSecurity(t *testing.T) {
	var spec bytes.Buffer
	if err := app.WriteOpenAPI(context.Background(), testConfig(t, nil), &spec); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			OperationID string                     `json:"operationId"`
			Security    []map[string][]string      `json:"security"`
			Responses   map[string]json.RawMessage `json:"responses"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(spec.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	operations := 0
	for path, methods := range doc.Paths {
		if !strings.HasPrefix(path, "/ops/") {
			continue
		}
		for method, op := range methods {
			operations++
			if len(op.Security) == 0 {
				t.Errorf("%s %s (%s) declares no security", strings.ToUpper(method), path, op.OperationID)
			}
			for _, status := range []string{"401", "403"} {
				if _, ok := op.Responses[status]; !ok {
					t.Errorf("%s %s (%s) doesn't declare a %s response", strings.ToUpper(method), path, op.OperationID, status)
				}
			}
		}
	}
	if operations < 20 {
		t.Errorf("found %d /ops operations in the spec", operations)
	}
}
