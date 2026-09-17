package main_test

import (
	"net/http"
	"slices"
	"testing"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/flagshttp"
	"gorbital.dev/gorbital/gorbitaltest"
	"gorbital.dev/gorbital/opshttp"

	"example.com/admin-tool/db/migrations"
	"example.com/admin-tool/internal/modules"
)

// newApp builds the admin tool with the options main.go passes to
// gorbital.Main.
func newApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	return gorbitaltest.New(t,
		gorbital.WithName("admin-tool"),
		gorbital.WithModules(opshttp.Module(), flagshttp.Module()),
		gorbital.WithModules(modules.All()...),
		gorbital.WithMigrations(migrations.FS),
	)
}

// retentionPolicy is a policy of GET /ops/retention.
type retentionPolicy struct {
	Data             string `json:"data"`
	Setting          string `json:"setting"`
	RetentionSeconds int64  `json:"retention_seconds"`
	Job              string `json:"job"`
}

// docs:start retention

// TestRetentionIsListed: GET /ops/retention shows the announcements
// module's data with its setting and the job that deletes it.
func TestRetentionIsListed(t *testing.T) {
	app := newApp(t)
	operator := app.As(gorbitaltest.User("usr_ops", "ops.settings.read"))

	var got struct {
		Policies []retentionPolicy `json:"policies"`
	}
	operator.Get("/ops/retention").JSON(t, &got)
	i := slices.IndexFunc(got.Policies, func(p retentionPolicy) bool { return p.Data == "expired_announcements" })
	if i < 0 {
		t.Fatalf("GET /ops/retention = %+v, want expired_announcements", got.Policies)
	}
	if p := got.Policies[i]; p.Setting != "announcements.retention" || p.Job != "retention" || p.RetentionSeconds != 90*24*3600 {
		t.Errorf("expired_announcements = %+v, want announcements.retention, 90 days, deleted by the retention job", p)
	}
	// Without ops.settings.read.
	app.As(gorbitaltest.User("usr_customer")).Get("/ops/retention").AssertProblem(t, http.StatusForbidden, "forbidden")
}

// docs:end retention

// docs:start rate-limits

// TestPublishLimiterIsListed: the named limiter of guard.RateLimit on
// POST /v1/announcements is in GET /ops/auth/rate-limits, where operators
// reset a key's budget.
func TestPublishLimiterIsListed(t *testing.T) {
	app := newApp(t)
	operator := app.As(gorbitaltest.User("usr_ops", "ops.auth.read"))

	var got struct {
		Limiters []struct{ Name, Keys, Description string } `json:"limiters"`
	}
	operator.Get("/ops/auth/rate-limits").JSON(t, &got)
	for _, l := range got.Limiters {
		if l.Name == "announcements_publish" {
			return
		}
	}
	t.Errorf("GET /ops/auth/rate-limits = %+v, want announcements_publish", got.Limiters)
}

// docs:end rate-limits

// docs:start flags

// TestBannerFlag: customer apps read announcements.banner from
// GET /v1/flags; it is off until an operator turns it on.
func TestBannerFlag(t *testing.T) {
	app := newApp(t)
	customer := app.As(gorbitaltest.User("usr_customer", flagshttp.PermRead))

	var got struct {
		Flags map[string]bool `json:"flags"`
	}
	customer.Get("/v1/flags").JSON(t, &got)
	if on, ok := got.Flags["announcements.banner"]; !ok || on {
		t.Errorf("GET /v1/flags = %v, want announcements.banner off", got.Flags)
	}
}

// docs:end flags
