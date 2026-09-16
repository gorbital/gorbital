package settings_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/actor"
	"gorbital.dev/modules/postgres/pgtest"
	"gorbital.dev/modules/settings"
)

// orgDeclared is the setting set used by organisation value tests.
type orgDeclared struct {
	reg      *settings.Registry
	greeting *settings.Setting[string]
	window   *settings.Setting[time.Duration]
	codeTTL  *settings.Setting[time.Duration]
}

func declareOrg() orgDeclared {
	reg := settings.NewRegistry()
	return orgDeclared{
		reg:      reg,
		greeting: settings.String(reg, "example.greeting", "hello", settings.MaxLen(20), settings.OrgOverridable()),
		codeTTL: settings.Duration(reg, "auth.verification_code_ttl", 15*time.Minute,
			settings.Range(5*time.Minute, time.Hour), settings.ReasonRequired()),
		window: settings.Duration(reg, "orgs.invitation_ttl", 7*24*time.Hour,
			settings.Range(24*time.Hour, 30*24*time.Hour), settings.ReasonRequired(), settings.OrgOverridable()),
	}
}

// inOrg is operator() acting in orgID, as orgs.RequireMember leaves it.
func inOrg(orgID string) context.Context {
	a, _ := actor.From(operator())
	a.OrgID = orgID
	return actor.With(operator(), a)
}

func newOrgStore(t *testing.T, pool *pgxpool.Pool, d orgDeclared, opts ...settings.StoreOption) (*settings.Store, *recorded) {
	t.Helper()
	rec := &recorded{}
	store, err := settings.NewStore(context.Background(), pool, d.reg, rec, opts...)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	return store, rec
}

func TestOrgOverridableCantBeRestartRequired(t *testing.T) {
	defer func() {
		if msg, _ := recover().(string); !strings.Contains(msg, "can't be RestartRequired") {
			t.Errorf("panic = %q, want can't be RestartRequired", msg)
		}
	}()
	settings.Int(settings.NewRegistry(), "jobs.workers", 4, settings.OrgOverridable(), settings.RestartRequired())
}

func TestOrgValuesOverridePlatformValues(t *testing.T) {
	ctx := operator()
	pool := pgtest.New(t, pgtest.WithMigrations(settings.Migrations))
	d := declareOrg()
	store, rec := newOrgStore(t, pool, d)
	orgA, orgB := inOrg("org_a"), inOrg("org_b")

	if got := d.greeting.Get(orgA); got != "hello" {
		t.Errorf("Get() in an organisation without a value = %q, want the default", got)
	}
	views, err := store.ListForOrg("org_a")
	if err != nil || len(views) != 2 || views[0].Key != "example.greeting" || views[1].Key != "orgs.invitation_ttl" {
		t.Fatalf("ListForOrg() = %+v, %v; want only the OrgOverridable settings in declaration order", views, err)
	}
	if v := views[0]; v.OrgID != "org_a" || !v.OrgOverridable || v.Modified || v.Version != 0 || string(v.Value) != `"hello"` || string(v.PlatformValue) != `"hello"` {
		t.Errorf("initial organisation view = %+v", v)
	}
	if v := mustGet(t, store, "example.greeting"); !v.OrgOverridable || v.OrgID != "" || v.PlatformValue != nil {
		t.Errorf("platform view = %+v", v)
	}

	v, err := store.SetForOrg(orgA, "org_a", "example.greeting", json.RawMessage(`"bonjour"`), settings.Change{})
	if err != nil {
		t.Fatalf("SetForOrg() error = %v", err)
	}
	if !v.Modified || v.Version != 1 || string(v.Value) != `"bonjour"` || string(v.PlatformValue) != `"hello"` || v.UpdatedBy != "usr_ops" || v.OrgID != "org_a" {
		t.Errorf("view after SetForOrg = %+v", v)
	}
	if got := d.greeting.Get(orgA); got != "bonjour" {
		t.Errorf("Get() in org_a = %q, want bonjour", got)
	}
	if got := d.greeting.Get(orgB); got != "hello" {
		t.Errorf("Get() in org_b = %q, want the platform value", got)
	}
	if got := d.greeting.Get(ctx); got != "hello" {
		t.Errorf("Get() outside organisations = %q, want the platform value", got)
	}
	if v := mustGet(t, store, "example.greeting"); v.Modified || v.Version != 0 {
		t.Errorf("platform view after an organisation change = %+v, want unchanged", v)
	}

	// The platform value changes for organisations without their own value;
	// versions are counted per organisation.
	if _, err := store.Set(ctx, "example.greeting", json.RawMessage(`"hi"`), settings.Change{Version: 0}); err != nil {
		t.Fatal(err)
	}
	if got, gotA := d.greeting.Get(orgB), d.greeting.Get(orgA); got != "hi" || gotA != "bonjour" {
		t.Errorf("Get() after a platform change = org_b %q, org_a %q; want hi, bonjour", got, gotA)
	}
	if v, err := store.GetForOrg("org_a", "example.greeting"); err != nil || v.Version != 1 || string(v.PlatformValue) != `"hi"` {
		t.Errorf("GetForOrg(org_a) = %+v, %v", v, err)
	}
	if _, err := store.SetForOrg(orgA, "org_a", "example.greeting", json.RawMessage(`"salut"`), settings.Change{Version: 0}); !errors.Is(err, settings.ErrVersionConflict) {
		t.Errorf("SetForOrg() with a stale version error = %v, want ErrVersionConflict", err)
	}
	if _, err := store.SetForOrg(orgB, "org_b", "example.greeting", json.RawMessage(`"hallo"`), settings.Change{Version: 0}); err != nil {
		t.Errorf("SetForOrg(org_b, version 0) error = %v; versions are per organisation", err)
	}

	// Organisation changes are recorded in the organisation's history and
	// audit events.
	history, err := store.HistoryForOrg(ctx, "org_a", "example.greeting", 0, 10)
	if err != nil || len(history) != 1 || history[0].OrgID != "org_a" || string(history[0].NewValue) != `"bonjour"` {
		t.Errorf("HistoryForOrg(org_a) = %+v, %v", history, err)
	}
	if platform, err := store.History(ctx, "example.greeting", 0, 10); err != nil || len(platform) != 1 || platform[0].OrgID != "" {
		t.Errorf("History() = %+v, %v; want only the platform change", platform, err)
	}
	if len(rec.events) != 3 {
		t.Fatalf("recorded %d audit events, want 3", len(rec.events))
	}
	if e := rec.events[0]; e.Action != "settings.value.changed" || e.OrgID != "org_a" || e.Metadata["org_id"] != "org_a" || e.Metadata["version"] != int64(1) {
		t.Errorf("organisation audit event = %+v", e)
	}
	if e := rec.events[1]; e.OrgID != "" || e.Metadata["org_id"] != nil {
		t.Errorf("platform audit event = %+v, want no organisation", e)
	}

	overrides, err := store.Overrides(ctx, "example.greeting", "", 1)
	if err != nil || len(overrides) != 1 || overrides[0].OrgID != "org_a" || string(overrides[0].Value) != `"bonjour"` {
		t.Fatalf("Overrides(limit 1) = %+v, %v", overrides, err)
	}
	next, err := store.Overrides(ctx, "example.greeting", overrides[0].OrgID, 10)
	if err != nil || len(next) != 1 || next[0].OrgID != "org_b" || next[0].Version != 1 {
		t.Errorf("Overrides(after org_a) = %+v, %v", next, err)
	}

	// Resetting brings the platform value back, and isn't an override.
	v, err = store.ResetForOrg(orgA, "org_a", "example.greeting", settings.Change{Version: 1})
	if err != nil || v.Modified || v.Version != 2 || string(v.Value) != `"hi"` {
		t.Errorf("ResetForOrg() = %+v, %v", v, err)
	}
	if got := d.greeting.Get(orgA); got != "hi" {
		t.Errorf("Get() in org_a after reset = %q, want the platform value", got)
	}
	if all, err := store.Overrides(ctx, "example.greeting", "", 10); err != nil || len(all) != 1 || all[0].OrgID != "org_b" {
		t.Errorf("Overrides() after reset = %+v, %v; want only org_b", all, err)
	}
	if none, err := store.Overrides(ctx, "auth.verification_code_ttl", "", 10); err != nil || len(none) != 0 {
		t.Errorf("Overrides(not overridable) = %+v, %v; want none", none, err)
	}
}

func TestOrgValuesRejectInvalidChanges(t *testing.T) {
	ctx := inOrg("org_a")
	pool := pgtest.New(t, pgtest.WithMigrations(settings.Migrations))
	store, rec := newOrgStore(t, pool, declareOrg())
	change := settings.Change{Reason: "test"}

	tests := []struct {
		name  string
		err   error
		check func() error
	}{
		{"not overridable", settings.ErrNotOrgOverridable, func() error {
			_, err := store.SetForOrg(ctx, "org_a", "auth.verification_code_ttl", json.RawMessage(`"30m"`), change)
			return err
		}},
		{"unknown key", settings.ErrUnknownSetting, func() error {
			_, err := store.SetForOrg(ctx, "org_a", "example.nope", json.RawMessage(`1`), change)
			return err
		}},
		{"invalid value", settings.ErrInvalidValue, func() error {
			_, err := store.SetForOrg(ctx, "org_a", "orgs.invitation_ttl", json.RawMessage(`"1h"`), change)
			return err
		}},
		{"missing reason", settings.ErrReasonRequired, func() error {
			_, err := store.SetForOrg(ctx, "org_a", "orgs.invitation_ttl", json.RawMessage(`"48h"`), settings.Change{Reason: " "})
			return err
		}},
		{"no actor", settings.ErrActorRequired, func() error {
			_, err := store.SetForOrg(context.Background(), "org_a", "example.greeting", json.RawMessage(`"x"`), change)
			return err
		}},
		{"empty organisation", settings.ErrInvalidOrgID, func() error {
			_, err := store.SetForOrg(ctx, "", "example.greeting", json.RawMessage(`"x"`), change)
			return err
		}},
		{"organisation with a space", settings.ErrInvalidOrgID, func() error {
			_, err := store.ResetForOrg(ctx, "org a", "example.greeting", change)
			return err
		}},
		{"long organisation", settings.ErrInvalidOrgID, func() error {
			_, err := store.ListForOrg(strings.Repeat("o", 101))
			return err
		}},
		{"history of a setting not overridable", settings.ErrNotOrgOverridable, func() error {
			_, err := store.HistoryForOrg(ctx, "org_a", "auth.verification_code_ttl", 0, 10)
			return err
		}},
		{"get not overridable", settings.ErrNotOrgOverridable, func() error {
			_, err := store.GetForOrg("org_a", "auth.verification_code_ttl")
			return err
		}},
		{"overrides of an unknown key", settings.ErrUnknownSetting, func() error {
			_, err := store.Overrides(ctx, "example.nope", "", 10)
			return err
		}},
	}
	for _, tt := range tests {
		if err := tt.check(); !errors.Is(err, tt.err) {
			t.Errorf("%s: error = %v, want %v", tt.name, err, tt.err)
		}
	}
	var n int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM settings_values").Scan(&n); err != nil || n != 0 {
		t.Errorf("rejected changes stored %d rows (%v)", n, err)
	}
	if len(rec.events) != 0 {
		t.Errorf("rejected changes recorded %d audit events", len(rec.events))
	}
}

func TestOrgValuesLoadAtStartupAndLeaveWithTheirRows(t *testing.T) {
	ctx := operator()
	pool := pgtest.New(t, pgtest.WithMigrations(settings.Migrations))
	first, _ := newOrgStore(t, pool, declareOrg())
	if _, err := first.SetForOrg(inOrg("org_a"), "org_a", "orgs.invitation_ttl", json.RawMessage(`"48h"`), settings.Change{Reason: "shorter links"}); err != nil {
		t.Fatal(err)
	}
	if _, err := first.SetForOrg(inOrg("org_b"), "org_b", "example.greeting", json.RawMessage(`"hej"`), settings.Change{}); err != nil {
		t.Fatal(err)
	}
	// An organisation value that no longer passes validation, and one of a
	// setting that isn't OrgOverridable.
	if _, err := pool.Exec(ctx, `INSERT INTO settings_values (key, org_id, value, version, updated_by) VALUES
		('example.greeting', 'org_c', '"far too long for this setting"', 1, 'usr_old'),
		('auth.verification_code_ttl', 'org_a', '"30m"', 1, 'usr_old')`); err != nil {
		t.Fatal(err)
	}

	d := declareOrg()
	store, _ := newOrgStore(t, pool, d)
	if got := d.window.Get(inOrg("org_a")); got != 48*time.Hour {
		t.Errorf("Get() in org_a after startup = %v, want 48h", got)
	}
	if got := d.codeTTL.Get(inOrg("org_a")); got != 15*time.Minute {
		t.Errorf("Get() of a setting that isn't OrgOverridable = %v, want the platform value", got)
	}
	if got := d.greeting.Get(inOrg("org_c")); got != "hello" {
		t.Errorf("Get() with an invalid organisation value = %q, want the platform value", got)
	}
	if v, err := store.GetForOrg("org_c", "example.greeting"); err != nil || !v.InvalidStoredValue || v.Modified || v.Version != 1 {
		t.Errorf("GetForOrg(org_c) = %+v, %v; want the invalid value flagged", v, err)
	}

	// Organisations are purged with their rows (in apps, through a foreign
	// key); a reload forgets their values.
	if _, err := pool.Exec(ctx, `DELETE FROM settings_values WHERE org_id = 'org_a'`); err != nil {
		t.Fatal(err)
	}
	if err := store.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	if got := d.window.Get(inOrg("org_a")); got != 7*24*time.Hour {
		t.Errorf("Get() in a purged organisation after Reload = %v, want the platform value", got)
	}
	if got := d.greeting.Get(inOrg("org_b")); got != "hej" {
		t.Errorf("Get() in org_b after Reload = %q, want hej", got)
	}
}

func TestOrgValuesConvergeThroughNotifications(t *testing.T) {
	ctx := operator()
	pool := pgtest.New(t, pgtest.WithMigrations(settings.Migrations))
	a, b := declareOrg(), declareOrg()
	storeA, _ := newOrgStore(t, pool, a)
	storeB, _ := newOrgStore(t, pool, b, settings.WithResyncInterval(time.Hour))

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- storeB.Run(runCtx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Run() error = %v", err)
		}
	}()
	waitFor(t, "listener to start", func() bool {
		var n int
		_ = pool.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE query = 'LISTEN gorbital_settings'").Scan(&n)
		return n > 0
	})

	if _, err := storeA.SetForOrg(inOrg("org_a"), "org_a", "example.greeting", json.RawMessage(`"ciao"`), settings.Change{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "instance B to see the organisation value", func() bool { return b.greeting.Get(inOrg("org_a")) == "ciao" })
	if got := b.greeting.Get(ctx); got != "hello" {
		t.Errorf("instance B platform value = %q, want unchanged", got)
	}
	if _, err := storeA.ResetForOrg(inOrg("org_a"), "org_a", "example.greeting", settings.Change{Version: 1}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "instance B to see the reset", func() bool { return b.greeting.Get(inOrg("org_a")) == "hello" })
}

func TestConcurrentOrgChangesOneWins(t *testing.T) {
	pool := pgtest.New(t, pgtest.WithMigrations(settings.Migrations), pgtest.WithMaxConns(10))
	store, _ := newOrgStore(t, pool, declareOrg())
	ctx := inOrg("org_a")

	const writers = 6
	errs := make([]error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Go(func() {
			value := json.RawMessage(`"one"`)
			if i%2 == 0 {
				value = json.RawMessage(`"two"`)
			}
			_, errs[i] = store.SetForOrg(ctx, "org_a", "example.greeting", value, settings.Change{Version: 0})
		})
	}
	wg.Wait()
	succeeded := 0
	for i, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case !errors.Is(err, settings.ErrVersionConflict):
			t.Errorf("writer %d: error = %v, want nil or ErrVersionConflict", i, err)
		}
	}
	if succeeded != 1 {
		t.Errorf("%d concurrent first organisation changes succeeded, want exactly 1", succeeded)
	}
}

func BenchmarkSettingGet(b *testing.B) {
	d := declareOrg()
	platform, org := context.Background(), inOrg("org_a")
	b.Run("platform", func(b *testing.B) {
		for b.Loop() {
			_ = d.codeTTL.Get(platform)
		}
	})
	b.Run("org overridable, in an organisation", func(b *testing.B) {
		for b.Loop() {
			_ = d.window.Get(org)
		}
	})
}
