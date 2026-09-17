package app_test

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"example.com/acme-api/internal/app"
)

func TestMaintenanceMode(t *testing.T) {
	a := newApp(t, nil)
	h := a.Handler()
	admin, _ := signIn(t, a, "admin@example.com", "platform_admin")
	if r := do(t, h, "GET", "/v1/ping", ""); r.code != http.StatusOK {
		t.Fatalf("GET /v1/ping before maintenance = %d", r.code)
	}

	if r := do(t, h, "PUT", "/ops/settings/maintenance.message", `{"value":"Upgrading the database","version":0}`, admin...); r.code != http.StatusOK {
		t.Fatalf("PUT maintenance.message = %d %s", r.code, r.body)
	}
	if r := do(t, h, "PUT", "/ops/settings/maintenance.enabled", `{"value":true,"version":0}`, admin...); r.code != http.StatusUnprocessableEntity {
		t.Errorf("PUT maintenance.enabled without a reason = %d, want 422", r.code)
	}
	if r := do(t, h, "PUT", "/ops/settings/maintenance.enabled", `{"value":true,"version":0,"reason":"database upgrade"}`, admin...); r.code != http.StatusOK {
		t.Fatalf("PUT maintenance.enabled = %d %s", r.code, r.body)
	}

	r := do(t, h, "GET", "/v1/ping", "")
	if r.code != http.StatusServiceUnavailable || r.json["code"] != "maintenance" || r.json["detail"] != "Upgrading the database" || r.header.Get("Retry-After") != "300" {
		t.Errorf("GET /v1/ping in maintenance = %d %s, Retry-After %q; want 503 maintenance with the message and 300", r.code, r.body, r.header.Get("Retry-After"))
	}
	for _, path := range []string{"/livez", "/readyz", "/version", "/openapi.json", "/docs"} {
		if r := do(t, h, "GET", path, ""); r.code != http.StatusOK {
			t.Errorf("GET %s in maintenance = %d, want 200", path, r.code)
		}
	}
	if r := do(t, h, "GET", "/ops/settings/maintenance.enabled", "", admin...); r.code != http.StatusOK {
		t.Errorf("GET /ops/settings in maintenance = %d, want 200", r.code)
	}
	if r := do(t, h, "POST", "/v1/auth/login", `{"email":"admin@example.com","password":"wrong password","transport":"bearer"}`); r.code == http.StatusServiceUnavailable {
		t.Errorf("POST /v1/auth/login in maintenance = 503, want sign-in to stay open")
	}

	if r := do(t, h, "PUT", "/ops/settings/maintenance.enabled", `{"value":false,"version":1,"reason":"done"}`, admin...); r.code != http.StatusOK {
		t.Fatalf("PUT maintenance.enabled off = %d %s", r.code, r.body)
	}
	if r := do(t, h, "GET", "/v1/ping", ""); r.code != http.StatusOK {
		t.Errorf("GET /v1/ping after maintenance = %d, want 200", r.code)
	}
}

// TestMaintenanceCommand: the break-glass command stores the setting, so an
// instance starting afterwards is in maintenance with the message.
func TestMaintenanceCommand(t *testing.T) {
	_, url := newAppWithURL(t, nil)
	ctx := context.Background()
	cfg := testConfig(t, map[string]string{"DATABASE_URL": url})
	var out bytes.Buffer
	if err := app.SetMaintenance(ctx, cfg, true, "Back at 10:00 UTC", &out); err != nil {
		t.Fatalf("SetMaintenance(on) error = %v", err)
	}
	if !strings.Contains(out.String(), "maintenance mode is on") {
		t.Errorf("output = %q", out.String())
	}

	started, err := app.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer started.Close(ctx)
	if r := do(t, started.Handler(), "GET", "/v1/ping", ""); r.code != http.StatusServiceUnavailable || r.json["detail"] != "Back at 10:00 UTC" {
		t.Errorf("GET /v1/ping on an instance started in maintenance = %d %s", r.code, r.body)
	}

	if err := app.SetMaintenance(ctx, cfg, false, "", &out); err != nil {
		t.Fatalf("SetMaintenance(off) error = %v", err)
	}
	restarted, err := app.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close(ctx)
	if r := do(t, restarted.Handler(), "GET", "/v1/ping", ""); r.code != http.StatusOK {
		t.Errorf("GET /v1/ping after maintenance off = %d, want 200", r.code)
	}
}
