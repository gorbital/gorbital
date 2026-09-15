package app_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"apistock.dev/modules/postgres/pgtest"

	"example.com/acme-api/internal/app"
)

// seedOutput returns a value printed by Seed after label, such as
// "Password:".
func seedOutput(out, label string) string {
	for line := range strings.Lines(out) {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), label); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func TestSeed(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t, map[string]string{"DATABASE_URL": pgtest.NewDatabase(t)})
	if err := app.Migrate(ctx, cfg, io.Discard); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	var out bytes.Buffer
	if err := app.Seed(ctx, cfg, app.DefaultSeedEmail, &out); err != nil {
		t.Fatalf("Seed() error = %v", err)
	}
	password, secret := seedOutput(out.String(), "Password:"), seedOutput(out.String(), "2FA key:")
	if len(password) < 20 || len(secret) != 32 || !strings.Contains(out.String(), "admin@example.com (platform_admin)") ||
		!strings.HasPrefix(seedOutput(out.String(), "2FA QR code URI:"), "otpauth://totp/") || len(strings.Fields(seedOutput(out.String(), "Recovery codes:"))) != 5 {
		t.Fatalf("Seed() output = %q, want the administrator, a strong password, a 2FA key and recovery codes", out.String())
	}

	a, err := app.New(ctx, cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { _ = a.Close(ctx) })
	h := a.Handler()

	// The administrator signs in with the printed password and a code from
	// the printed key, and the ops role applies.
	bearer := []string{"Authorization", "Bearer " + signInWithTOTP(t, h, app.DefaultSeedEmail, password, secret)}
	if r := do(t, h, "GET", "/ops/settings", "", bearer...); r.code != http.StatusOK {
		t.Errorf("GET /ops/settings as the seeded administrator = %d %s, want 200", r.code, r.body)
	}
	// The examples are in the administrator's personal workspace.
	workspace := personalWorkspace(t, h, bearer)
	if printed := seedOutput(out.String(), "Workspace:"); !strings.HasPrefix(printed, workspace+" ") {
		t.Errorf("Seed() printed workspace %q, want %s", printed, workspace)
	}
	collection := "/v1/orgs/" + workspace + "/projects?limit=50"
	projects := do(t, h, "GET", collection, "", bearer...)
	for _, name := range []string{"Website redesign", "Mobile app", "Legacy import"} {
		if strings.Count(projects.body, fmt.Sprintf("%q", name)) != 1 {
			t.Errorf("GET %s = %s, want the example project %q once", collection, projects.body, name)
		}
	}

	// Running it again changes nothing and prints no secrets.
	out.Reset()
	if err := app.Seed(ctx, cfg, app.DefaultSeedEmail, &out); err != nil {
		t.Fatalf("Seed() again error = %v", err)
	}
	if s := out.String(); !strings.Contains(s, "left unchanged") || strings.Contains(s, "Password:") || strings.Contains(s, "2FA key:") {
		t.Errorf("Seed() again output = %q", s)
	}
	again := do(t, h, "GET", collection, "", bearer...)
	if strings.Count(again.body, `"Legacy import"`) != 1 {
		t.Errorf("GET %s after seeding twice = %s, want each example once", collection, again.body)
	}
	if r := do(t, h, "POST", "/v1/auth/login", fmt.Sprintf(`{"email":%q,"password":%q}`, app.DefaultSeedEmail, password)); r.code != http.StatusAccepted {
		t.Errorf("login after seeding twice = %d %s, want the password and 2FA unchanged", r.code, r.body)
	}
}

func TestSeedRefusesProduction(t *testing.T) {
	cfg := testConfig(t, map[string]string{"DATABASE_URL": "postgres://unused@127.0.0.1:1/unused"})
	cfg.Env = "production"
	err := app.Seed(context.Background(), cfg, app.DefaultSeedEmail, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "production") {
		t.Errorf("Seed() in production = %v, want a refusal", err)
	}
}

func TestSeedNeedsEncryptionKeys(t *testing.T) {
	cfg := testConfig(t, map[string]string{"DATABASE_URL": "postgres://unused@127.0.0.1:1/unused", "AUTH_ENCRYPTION_KEYS": ""})
	err := app.Seed(context.Background(), cfg, app.DefaultSeedEmail, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "AUTH_ENCRYPTION_KEYS") {
		t.Errorf("Seed() without encryption keys = %v, want a message naming AUTH_ENCRYPTION_KEYS", err)
	}
}
