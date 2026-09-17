package main_test

import (
	"net/http"
	"slices"
	"testing"
	"time"

	"gorbital.dev/gorbital"
	"gorbital.dev/gorbital/flagshttp"
	"gorbital.dev/gorbital/gorbitaltest"
	"gorbital.dev/gorbital/opshttp"
	"gorbital.dev/modules/auth"

	"example.com/admin-tool/db/migrations"
	"example.com/admin-tool/internal/modules"
	"example.com/admin-tool/internal/modules/announcements/usecase"
)

// newApp builds the admin tool with the options main.go passes to
// gorbital.Main, signed in as gorbitaltest's principals instead of
// sign-in's accounts; signin_test.go builds it with sign-in.
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

// auditEvent is an event of GET /ops/audit.
type auditEvent struct {
	Action     string         `json:"action"`
	ActorKind  string         `json:"actor_kind"`
	ActorID    string         `json:"actor_id"`
	Outcome    string         `json:"outcome"`
	Resource   string         `json:"resource_id"`
	RequestID  string         `json:"request_id"`
	OccurredAt time.Time      `json:"occurred_at"`
	Metadata   map[string]any `json:"metadata"`
}

// events reads GET /ops/audit with the query, newest first.
func events(t *testing.T, operator *gorbitaltest.Client, query string) []auditEvent {
	t.Helper()
	var page struct {
		Events []auditEvent `json:"events"`
	}
	operator.Get("/ops/audit?"+query).JSON(t, &page)
	return page.Events
}

// docs:start audit-setting-change

// TestSettingChangesSayWhy: announcements.max_active is declared with
// settings.ReasonRequired, so a change without a reason is refused and the
// audit event that records the change keeps the one that was given.
func TestSettingChangesSayWhy(t *testing.T) {
	app := newApp(t)
	operator := app.As(gorbitaltest.User("usr_ops", "ops.settings.read", "ops.settings.write", "ops.audit.read"))

	operator.Put("/ops/settings/announcements.max_active", map[string]any{"value": 1, "version": 0}).
		AssertProblem(t, http.StatusUnprocessableEntity, "setting_reason_required")
	operator.Put("/ops/settings/announcements.max_active", map[string]any{"value": 1, "version": 0, "reason": "one banner at a time"}).
		AssertStatus(t, http.StatusOK)

	got := events(t, operator, "action=settings.value.changed&resource_id=announcements.max_active")
	if len(got) != 1 {
		t.Fatalf("GET /ops/audit = %+v, want the one change", got)
	}
	if e := got[0]; e.ActorKind != "user" || e.ActorID != "usr_ops" || e.Outcome != "success" ||
		e.Metadata["reason"] != "one banner at a time" || e.Metadata["version"] != float64(1) {
		t.Errorf("settings.value.changed = %+v", e)
	}
}

// docs:end audit-setting-change

// docs:start audit-withdrawal

// TestWithdrawalsAreAudited: withdrawing deletes the announcement, so the
// audit log is where the withdrawal, its reason and who did it survive.
func TestWithdrawalsAreAudited(t *testing.T) {
	app := newApp(t)
	staff := app.As(gorbitaltest.User("usr_ada", usecase.PermWrite))
	id := publish(t, staff)

	staff.Post("/v1/announcements/"+id+"/withdraw", map[string]string{"reason": "the maintenance window moved"}).
		AssertStatus(t, http.StatusNoContent)
	var list struct {
		Items []struct{} `json:"items"`
	}
	app.Client().Get("/v1/announcements").JSON(t, &list)
	if len(list.Items) != 0 {
		t.Errorf("GET /v1/announcements = %d announcements, want none after the withdrawal", len(list.Items))
	}

	operator := app.As(gorbitaltest.User("usr_ops", "ops.audit.read"))
	got := events(t, operator, "action_prefix=announcements.&resource_id="+id)
	if len(got) != 2 || got[0].Action != usecase.ActionWithdrawn || got[1].Action != usecase.ActionPublished {
		t.Fatalf("GET /ops/audit = %+v, want the withdrawal then the publication", got)
	}
	if e := got[0]; e.ActorID != "usr_ada" || e.Resource != id || e.Metadata["reason"] != "the maintenance window moved" {
		t.Errorf("%s = %+v", usecase.ActionWithdrawn, e)
	}
	// Every event of the withdrawal's request, which the access log line,
	// the trace and any job it enqueued carry too.
	if byRequest := events(t, operator, "request_id="+got[0].RequestID); len(byRequest) != 1 {
		t.Errorf("GET /ops/audit?request_id= = %+v, want the one event of the withdrawal's request", byRequest)
	}
}

// docs:end audit-withdrawal

// docs:start withdraw-guards

// TestWithdrawingNeedsARecentSession: guard.RecentReauth on the withdrawal
// refuses API keys outright and sessions that signed in a while ago, both
// before the announcement is looked up.
func TestWithdrawingNeedsARecentSession(t *testing.T) {
	app := newApp(t)
	staff := app.As(gorbitaltest.User("usr_ada", usecase.PermWrite))
	id := publish(t, staff)
	withdraw := "/v1/announcements/" + id + "/withdraw"
	reason := map[string]string{"reason": "published by mistake"}

	app.Client().Post(withdraw, reason).AssertProblem(t, http.StatusUnauthorized, "unauthenticated")
	app.As(gorbitaltest.User("usr_bob")).Post(withdraw, reason).AssertProblem(t, http.StatusForbidden, "forbidden")
	app.As(gorbitaltest.APIKey("usr_ada", usecase.PermWrite)).Post(withdraw, reason).
		AssertProblem(t, http.StatusForbidden, "session_required")
	stale := auth.Principal{
		UserID: "usr_ada", SessionID: "ses_this_morning", Permissions: []string{usecase.PermWrite},
		SignedInAt: time.Now().Add(-8 * time.Hour),
	}
	app.As(stale).Post(withdraw, reason).AssertProblem(t, http.StatusForbidden, "reauthentication_required")

	// The reason is checked, and a withdrawal can't be replayed.
	staff.Post(withdraw, map[string]string{"reason": "   "}).AssertProblem(t, http.StatusUnprocessableEntity, "withdrawal_reason_required")
	staff.Post(withdraw, reason).AssertStatus(t, http.StatusNoContent)
	staff.Post(withdraw, reason).AssertProblem(t, http.StatusNotFound, "announcement_not_found")
}

// docs:end withdraw-guards

// publish publishes an announcement as client and returns its ID.
func publish(t *testing.T, client *gorbitaltest.Client) string {
	t.Helper()
	res := client.Post("/v1/announcements", map[string]any{
		"title": "Scheduled maintenance", "body": "Saturday, 02:00 to 03:00 UTC.", "ends_at": time.Now().Add(time.Hour),
	})
	res.AssertStatus(t, http.StatusCreated)
	var created struct {
		ID string `json:"id"`
	}
	res.JSON(t, &created)
	return created.ID
}
