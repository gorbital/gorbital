package main_test

import (
	"net/http"
	"slices"
	"testing"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/flagshttp"
	"gorbital.dev/gorbital/gorbitaltest"
	"gorbital.dev/gorbital/opshttp"

	"example.com/shelfie/db/migrations"
	"example.com/shelfie/internal/modules"
	"example.com/shelfie/internal/modules/books/usecase"
)

// newApp builds Shelfie with the options main.go passes to gorbital.Main.
func newApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	return gorbitaltest.New(t,
		gorbital.WithName("shelfie"),
		gorbital.WithModules(opshttp.Module(), flagshttp.Module()),
		gorbital.WithModules(modules.All()...),
		gorbital.WithMigrations(migrations.FS),
	)
}

// The permissions of the roles opshttp declares, as a signed-in operator's
// session holds them.
var (
	opsViewer = []string{"ops.settings.read", "ops.jobs.read", "ops.audit.read", "ops.flags.read", "ops.system.read", "ops.auth.read"}
	opsAdmin  = append(slices.Clone(opsViewer), "ops.settings.write", "ops.flags.write", "ops.jobs.write", "ops.jobs.run")
)

// docs:start ops-permissions

func TestOperationsNeedTheirPermissions(t *testing.T) {
	app := newApp(t)
	reader := app.As(gorbitaltest.User("usr_ada", usecase.PermRead, usecase.PermWrite, flagshttp.PermRead))
	viewer := app.As(gorbitaltest.User("usr_viewer", opsViewer...))

	app.Client().Get("/ops/settings").AssertProblem(t, http.StatusUnauthorized, "unauthenticated")
	reader.Get("/ops/settings").AssertProblem(t, http.StatusForbidden, "forbidden")
	viewer.Get("/ops/settings").AssertStatus(t, http.StatusOK)
	viewer.Put("/ops/settings/books.shelf_limit", map[string]any{"value": 10, "version": 0, "reason": "test"}).
		AssertProblem(t, http.StatusForbidden, "forbidden")

	// The jobs gorbital defines are operated through /ops/jobs.
	var jobs struct {
		Definitions []struct{ Name string } `json:"definitions"`
	}
	viewer.Get("/ops/jobs/definitions").JSON(t, &jobs)
	if !slices.ContainsFunc(jobs.Definitions, func(d struct{ Name string }) bool { return d.Name == "retention" }) {
		t.Errorf("GET /ops/jobs/definitions = %+v, want the retention job", jobs.Definitions)
	}
}

// docs:end ops-permissions

// docs:start flags

func TestReadingGoalsFlag(t *testing.T) {
	app := newApp(t)
	reader := app.As(gorbitaltest.User("usr_ada", flagshttp.PermRead))
	admin := app.As(gorbitaltest.User("usr_admin", opsAdmin...))

	var got struct {
		Flags map[string]bool `json:"flags"`
	}
	reader.Get("/v1/flags").JSON(t, &got)
	if on, ok := got.Flags["books.reading_goals"]; !ok || on {
		t.Fatalf("GET /v1/flags = %v, want books.reading_goals off", got.Flags)
	}

	// On for everyone: no percentage rollout, no organisations or users
	// targeted.
	on := map[string]any{"enabled": true, "default": true, "orgs": map[string][]string{"allow": {}, "deny": {}}, "users": map[string][]string{"allow": {}, "deny": {}}}
	admin.Put("/ops/flags/books.reading_goals", map[string]any{"state": on, "version": 0, "reason": "launch"}).
		AssertStatus(t, http.StatusOK)
	reader.Get("/v1/flags").JSON(t, &got)
	if !got.Flags["books.reading_goals"] {
		t.Errorf("GET /v1/flags after turning it on = %v", got.Flags)
	}
	// An API key without flags.flag.read in its scopes can't read them.
	app.As(gorbitaltest.APIKey("usr_ada", usecase.PermRead)).Get("/v1/flags").AssertProblem(t, http.StatusForbidden, "forbidden")
}

// docs:end flags

func TestOperationsReportTheApp(t *testing.T) {
	app := newApp(t)
	viewer := app.As(gorbitaltest.User("usr_viewer", opsViewer...))

	// The books module's guard.RateLimit is listed, so operators can reset
	// a reader's budget.
	var limits struct {
		Limiters []struct{ Name, Keys, Description string } `json:"limiters"`
	}
	viewer.Get("/ops/auth/rate-limits").JSON(t, &limits)
	var names []string
	for _, l := range limits.Limiters {
		names = append(names, l.Name)
	}
	if !slices.Contains(names, "auth_ip") || !slices.Contains(names, "ops_test_email") || !slices.Contains(names, "books-post-v1-books") {
		t.Errorf("GET /ops/auth/rate-limits = %v, want the built-in, ops and books limiters", names)
	}

	var system struct {
		Database struct {
			Status     string `json:"status"`
			Migrations struct {
				Pending int `json:"pending"`
			} `json:"migrations"`
		} `json:"database"`
	}
	viewer.Get("/ops/system").JSON(t, &system)
	if system.Database.Status != "ok" || system.Database.Migrations.Pending != 0 {
		t.Errorf("GET /ops/system database = %+v, want ok with no pending migration", system.Database)
	}
}
