package opshttp_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"gorbital.dev/gorbital/internal/opstest"
)

// These tests are a v0.1 golden app's internal/app/ops_system_test.go, run
// against the library module.

func TestOpsSystem(t *testing.T) {
	a := opstest.New(t, opstest.Options{})
	h := a.Handler()
	if r := opstest.Do(t, h, "GET", "/ops/system", ""); r.Code != http.StatusUnauthorized {
		t.Errorf("GET /ops/system without a session = %d, want 401", r.Code)
	}
	viewer, _ := a.SignIn(t, "viewer@example.com", "ops_viewer")
	r := opstest.Do(t, h, "GET", "/ops/system", "", viewer...)
	if r.Code != http.StatusOK {
		t.Fatalf("GET /ops/system as ops_viewer = %d %s", r.Code, r.Body)
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
	if err := json.Unmarshal([]byte(r.Body), &body); err != nil {
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
		if strings.Contains(r.Body, secret) {
			t.Errorf("GET /ops/system response contains %q", secret)
		}
	}
}

// TestOpsOperationsDeclareSecurity checks the contract every /ops endpoint
// promises: a bearer session, 401 without one and 403 without the
// permission or two-factor authentication (ADR-0026, ADR-0051). The
// use-case test TestEveryOperationAuthorizesFirst checks the behaviour.
func TestOpsOperationsDeclareSecurity(t *testing.T) {
	// The golden test wrote the app's OpenAPI document with app.WriteOpenAPI;
	// here the app serves it.
	a := opstest.New(t, opstest.Options{})
	served := opstest.Do(t, a.Handler(), "GET", "/openapi.json", "")
	if served.Code != http.StatusOK {
		t.Fatalf("GET /openapi.json = %d %s", served.Code, served.Body)
	}
	var doc struct {
		Paths map[string]map[string]struct {
			OperationID string                     `json:"operationId"`
			Security    []map[string][]string      `json:"security"`
			Responses   map[string]json.RawMessage `json:"responses"`
		} `json:"paths"`
	}
	if err := json.Unmarshal([]byte(served.Body), &doc); err != nil {
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
