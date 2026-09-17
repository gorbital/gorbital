package announcements_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/gorbitaltest"
	"gorbital.dev/gorbital/opshttp"

	"example.com/admin-tool/db/migrations"
	"example.com/admin-tool/internal/modules/announcements"
	"example.com/admin-tool/internal/modules/announcements/usecase"
)

// newApp builds the admin tool with the operations API and the
// announcements module on a new database for the test.
func newApp(t *testing.T, modules ...gorbital.Module) *gorbitaltest.App {
	t.Helper()
	if len(modules) == 0 {
		modules = []gorbital.Module{announcements.Module()}
	}
	return gorbitaltest.New(t,
		gorbital.WithModules(opshttp.Module()),
		gorbital.WithModules(modules...),
		gorbital.WithMigrations(migrations.FS),
	)
}

type announcement struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	PublishedBy string    `json:"published_by"`
	EndsAt      time.Time `json:"ends_at"`
}

func nextWeek() string { return time.Now().Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339) }

// docs:start publish-and-read

// TestPublishAndRead: staff publish, anyone reads, customers can't publish.
func TestPublishAndRead(t *testing.T) {
	app := newApp(t)
	// Staff: a signed-in user whose platform_admin role grants the
	// permission. Sign-in, which grants roles, arrives in Phase 5.
	staff := app.As(gorbitaltest.User("usr_staff", usecase.PermWrite))
	customer := app.As(gorbitaltest.User("usr_customer"))

	res := staff.Post("/v1/announcements", map[string]string{
		"title": "Scheduled maintenance", "body": "Saturday, 02:00 to 03:00 UTC.", "ends_at": nextWeek(),
	})
	res.AssertStatus(t, http.StatusCreated)
	var published announcement
	res.JSON(t, &published)
	if published.PublishedBy != "usr_staff" {
		t.Errorf("published_by = %q, want usr_staff", published.PublishedBy)
	}

	// Reading is public: nobody signed in.
	var list struct{ Items []announcement }
	app.Client().Get("/v1/announcements").JSON(t, &list)
	if len(list.Items) != 1 || list.Items[0].ID != published.ID {
		t.Errorf("GET /v1/announcements = %+v, want the published announcement", list.Items)
	}

	body := map[string]string{"title": "Hello", "body": "Hi", "ends_at": nextWeek()}
	customer.Post("/v1/announcements", body).AssertProblem(t, http.StatusForbidden, "forbidden")
	app.Client().Post("/v1/announcements", body).AssertProblem(t, http.StatusUnauthorized, "unauthenticated")
	staff.Post("/v1/announcements", map[string]string{"title": "Late", "body": "Hi", "ends_at": "2020-01-01T00:00:00Z"}).
		AssertProblem(t, http.StatusUnprocessableEntity, "invalid_ends_at")

	var events int
	if err := app.App().Deps().DB.QueryRow(context.Background(),
		`SELECT count(*) FROM audit_events WHERE action = $1 AND actor_id = 'usr_staff'`, usecase.ActionPublished).Scan(&events); err != nil || events != 1 {
		t.Errorf("audit events = %d, %v; want 1", events, err)
	}
}

// docs:end publish-and-read

// docs:start max-active

// TestMaxActiveFromOps: an operator lowers announcements.max_active through
// the operations API, and the next publish over the limit is refused,
// without a restart.
func TestMaxActiveFromOps(t *testing.T) {
	app := newApp(t)
	staff := app.As(gorbitaltest.User("usr_staff", usecase.PermWrite))
	operator := app.As(gorbitaltest.User("usr_ops", "ops.settings.read", "ops.settings.write"))

	staff.Post("/v1/announcements", map[string]string{"title": "One", "body": "First", "ends_at": nextWeek()}).
		AssertStatus(t, http.StatusCreated)

	// The reason is required; version 0 is the setting never changed.
	operator.Put("/ops/settings/announcements.max_active", map[string]any{"value": 1, "version": 0, "reason": "one banner at a time"}).
		AssertStatus(t, http.StatusOK)

	staff.Post("/v1/announcements", map[string]string{"title": "Two", "body": "Second", "ends_at": nextWeek()}).
		AssertProblem(t, http.StatusConflict, "too_many_announcements")
	// Out of the declared range.
	operator.Put("/ops/settings/announcements.max_active", map[string]any{"value": 50, "version": 1, "reason": "more"}).
		AssertProblem(t, http.StatusUnprocessableEntity, "invalid_setting_value")
}

// docs:end max-active

// docs:start retention-delete

// TestRetentionDeletesExpired calls the Delete function the module gives the
// retention job, since workers don't run in tests. The module value is the
// one the app was built with: its settings are declared by then.
func TestRetentionDeletesExpired(t *testing.T) {
	module := announcements.Module()
	app := newApp(t, module)
	ctx := context.Background()
	db := app.App().Deps().DB

	now := time.Now().UTC().Truncate(time.Microsecond)
	for id, ended := range map[string]time.Duration{"ann_old": 200 * 24 * time.Hour, "ann_recent": 24 * time.Hour, "ann_active": -24 * time.Hour} {
		endsAt := now.Add(-ended)
		if _, err := db.Exec(ctx, `INSERT INTO announcements (id, title, body, published_by, starts_at, ends_at, created_at)
			VALUES ($1, 'Title', 'Body', 'usr_staff', $2, $3, $2)`, id, endsAt.Add(-time.Hour), endsAt); err != nil {
			t.Fatal(err)
		}
	}

	policies := module.Retention(app.App().Deps())
	if len(policies) != 1 || policies[0].Data != "expired_announcements" {
		t.Fatalf("Retention = %+v", policies)
	}
	p := policies[0]
	if oldest, ok, err := p.Oldest(ctx); err != nil || !ok || !oldest.Equal(now.Add(-200*24*time.Hour)) {
		t.Errorf("Oldest = %v, %v, %v; want ann_old's end", oldest, ok, err)
	}

	// The job passes the time the setting's value ago: 90 days by default.
	before := now.Add(-p.Setting.Get(ctx))
	if n, err := p.Delete(ctx, before, 5000); err != nil || n != 1 {
		t.Errorf("Delete(90 days ago) = %d, %v; want ann_old only", n, err)
	}
	// Everything that has ended, in batches of one.
	for _, want := range []int64{1, 0} {
		if n, err := p.Delete(ctx, now, 1); err != nil || n != want {
			t.Errorf("Delete(now, 1) = %d, %v; want %d", n, err, want)
		}
	}
	var left []string
	rows, _ := db.Query(ctx, `SELECT id FROM announcements`)
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		left = append(left, id)
	}
	if rows.Err() != nil || len(left) != 1 || left[0] != "ann_active" {
		t.Errorf("left = %v, %v; want only ann_active", left, rows.Err())
	}
}

// docs:end retention-delete
