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

	"apistock.dev/actor"
	"apistock.dev/audit"
	"apistock.dev/modules/postgres/pgtest"
	"apistock.dev/modules/settings"
	"apistock.dev/requestid"
)

// declared is the setting set used by store tests.
type declared struct {
	reg        *settings.Registry
	codeTTL    *settings.Setting[time.Duration]
	mode       *settings.Setting[string]
	origins    *settings.Setting[[]string]
	workers    *settings.Setting[int]
	maintained *settings.Setting[bool]
}

func declare() declared {
	reg := settings.NewRegistry()
	return declared{
		reg: reg,
		codeTTL: settings.Duration(reg, "auth.verification_code_ttl", 15*time.Minute,
			settings.Describe("How long email verification codes stay valid."),
			settings.Range(5*time.Minute, time.Hour),
			settings.ReasonRequired(),
		),
		mode:       settings.Enum(reg, "mail.delivery", "async", []string{"sync", "async"}, settings.Group("email")),
		origins:    settings.StringList(reg, "http.cors_origins", nil, settings.URL(), settings.MaxItems(3)),
		workers:    settings.Int(reg, "jobs.workers", 4, settings.Range(1, 64), settings.RestartRequired()),
		maintained: settings.Bool(reg, "ops.maintenance_mode", false),
	}
}

type recorded struct {
	mu     sync.Mutex
	events []audit.Event
}

func (r *recorded) Record(_ context.Context, e audit.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	return nil
}

func newStore(t *testing.T, pool *pgxpool.Pool, d declared, opts ...settings.StoreOption) (*settings.Store, *recorded) {
	t.Helper()
	rec := &recorded{}
	store, err := settings.NewStore(context.Background(), pool, d.reg, rec, opts...)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	return store, rec
}

func operator() context.Context {
	ctx := actor.With(context.Background(), actor.Actor{Kind: actor.KindUser, ID: "usr_ops", Label: "Ops"})
	return requestid.With(ctx, "req_123")
}

func mustGet(t *testing.T, store *settings.Store, key string) settings.View {
	t.Helper()
	v, err := store.Get(key)
	if err != nil {
		t.Fatalf("Get(%s) error = %v", key, err)
	}
	return v
}

func TestNewStoreValidatesDependencies(t *testing.T) {
	_, err := settings.NewStore(context.Background(), nil, nil, nil, settings.WithResyncInterval(0))
	if err == nil {
		t.Fatal("NewStore() error = nil")
	}
	for _, want := range []string{"pool is required", "registry is required", "audit recorder is required", "resync interval"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("NewStore() error = %q, want it to mention %q", err, want)
		}
	}
}

func TestSetResetAndHistory(t *testing.T) {
	ctx := operator()
	pool := pgtest.New(t, pgtest.WithMigrations(settings.Migrations))
	d := declare()
	store, rec := newStore(t, pool, d)

	list := store.List()
	if len(list) != 5 || list[0].Key != "auth.verification_code_ttl" || list[1].Group != "email" || list[2].Group != "http" {
		t.Fatalf("List() = %+v, want 5 settings in declaration order with groups", list)
	}
	v := list[0]
	if v.Modified || v.Version != 0 || string(v.Value) != `"15m0s"` || !v.ReasonRequired ||
		v.Constraints["min"] != "5m0s" || v.Constraints["max"] != "1h0m0s" {
		t.Errorf("initial view = %+v", v)
	}

	v, err := store.Set(ctx, "auth.verification_code_ttl", json.RawMessage(`"30m"`), settings.Change{Version: 0, Reason: "support backlog"})
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if !v.Modified || v.Version != 1 || string(v.Value) != `"30m0s"` || v.UpdatedBy != "usr_ops" {
		t.Errorf("view after Set = %+v", v)
	}
	if got := d.codeTTL.Get(ctx); got != 30*time.Minute {
		t.Errorf("Get() after Set = %v, want 30m", got)
	}

	if _, err := store.Set(ctx, "auth.verification_code_ttl", json.RawMessage(`"20m"`), settings.Change{Version: 0, Reason: "stale"}); !errors.Is(err, settings.ErrVersionConflict) {
		t.Errorf("Set() with a stale version error = %v, want ErrVersionConflict", err)
	}

	v, err = store.Reset(ctx, "auth.verification_code_ttl", settings.Change{Version: 1, Reason: "backlog cleared"})
	if err != nil {
		t.Fatalf("Reset() error = %v", err)
	}
	if v.Modified || v.Version != 2 || string(v.Value) != `"15m0s"` {
		t.Errorf("view after Reset = %+v", v)
	}
	if got := d.codeTTL.Get(ctx); got != 15*time.Minute {
		t.Errorf("Get() after Reset = %v, want the default", got)
	}
	// Resetting a setting already at its default changes nothing.
	if v, err := store.Reset(ctx, "auth.verification_code_ttl", settings.Change{Version: 2, Reason: "again"}); err != nil || v.Version != 2 {
		t.Errorf("second Reset() = version %d, %v; want no change", v.Version, err)
	}

	history, err := store.History(ctx, "auth.verification_code_ttl", 0, 10)
	if err != nil {
		t.Fatalf("History() error = %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("History() returned %d entries, want 2", len(history))
	}
	reset, set := history[0], history[1]
	if reset.Version != 2 || string(reset.OldValue) != `"30m0s"` || reset.NewValue != nil || reset.Reason != "backlog cleared" {
		t.Errorf("reset entry = %+v", reset)
	}
	if set.Version != 1 || set.OldValue != nil || string(set.NewValue) != `"30m0s"` ||
		set.ActorKind != actor.KindUser || set.ActorID != "usr_ops" || set.RequestID != "req_123" || set.ChangedAt.IsZero() {
		t.Errorf("set entry = %+v", set)
	}
	page, err := store.History(ctx, "auth.verification_code_ttl", reset.ID, 1)
	if err != nil || len(page) != 1 || page[0].ID != set.ID {
		t.Errorf("History(before=%d, limit=1) = %+v, %v; want the older entry", reset.ID, page, err)
	}

	if len(rec.events) != 2 {
		t.Fatalf("recorded %d audit events, want 2", len(rec.events))
	}
	if e := rec.events[0]; e.Action != "settings.value.changed" || e.ResourceID != "auth.verification_code_ttl" ||
		e.Outcome != audit.OutcomeSuccess || e.Metadata["version"] != int64(1) || e.Metadata["reset"] != false {
		t.Errorf("audit event = %+v", e)
	}
	if err := rec.events[0].Validate(); err != nil {
		t.Errorf("audit event is invalid: %v", err)
	}
}

func TestSetRejectsInvalidChanges(t *testing.T) {
	ctx := operator()
	pool := pgtest.New(t, pgtest.WithMigrations(settings.Migrations))
	store, rec := newStore(t, pool, declare())

	invalid := []struct {
		key, value string
	}{
		{"auth.verification_code_ttl", `"2h"`},
		{"auth.verification_code_ttl", `"soon"`},
		{"auth.verification_code_ttl", `900`},
		{"auth.verification_code_ttl", `null`},
		{"mail.delivery", `"carrier-pigeon"`},
		{"http.cors_origins", `["https://a.example.com", "ftp://b.example.com"]`},
		{"http.cors_origins", `["https://a.com", "https://b.com", "https://c.com", "https://d.com"]`},
		{"jobs.workers", `4.5`},
		{"ops.maintenance_mode", `"yes"`},
	}
	for _, tt := range invalid {
		_, err := store.Set(ctx, tt.key, json.RawMessage(tt.value), settings.Change{Reason: "test"})
		var invalidErr *settings.InvalidValueError
		if !errors.As(err, &invalidErr) || !errors.Is(err, settings.ErrInvalidValue) || invalidErr.Key != tt.key {
			t.Errorf("Set(%s, %s) error = %v, want InvalidValueError", tt.key, tt.value, err)
			continue
		}
		if strings.Contains(err.Error(), strings.Trim(tt.value, `"`)) {
			t.Errorf("Set(%s) error %q echoes the rejected value", tt.key, err)
		}
	}

	if _, err := store.Set(ctx, "auth.verification_code_ttl", json.RawMessage(`"30m"`), settings.Change{Reason: "  "}); !errors.Is(err, settings.ErrReasonRequired) {
		t.Errorf("Set() without reason error = %v, want ErrReasonRequired", err)
	}
	if _, err := store.Set(context.Background(), "ops.maintenance_mode", json.RawMessage(`true`), settings.Change{}); !errors.Is(err, settings.ErrActorRequired) {
		t.Errorf("Set() without actor error = %v, want ErrActorRequired", err)
	}
	anon := actor.With(context.Background(), actor.Anonymous)
	if _, err := store.Set(anon, "ops.maintenance_mode", json.RawMessage(`true`), settings.Change{}); !errors.Is(err, settings.ErrActorRequired) {
		t.Errorf("Set() as anonymous error = %v, want ErrActorRequired", err)
	}
	if _, err := store.Set(ctx, "auth.unknown", json.RawMessage(`1`), settings.Change{}); !errors.Is(err, settings.ErrUnknownSetting) {
		t.Errorf("Set(unknown) error = %v, want ErrUnknownSetting", err)
	}
	if _, err := store.Get("auth.unknown"); !errors.Is(err, settings.ErrUnknownSetting) {
		t.Errorf("Get(unknown) error = %v, want ErrUnknownSetting", err)
	}

	for _, v := range store.List() {
		if v.Version != 0 {
			t.Errorf("%s changed to version %d by a rejected change", v.Key, v.Version)
		}
	}
	if len(rec.events) != 0 {
		t.Errorf("rejected changes recorded %d audit events", len(rec.events))
	}
}

func TestSettingTheSameValueIsANoOp(t *testing.T) {
	ctx := operator()
	pool := pgtest.New(t, pgtest.WithMigrations(settings.Migrations))
	store, rec := newStore(t, pool, declare())

	value := json.RawMessage(`["https://a.example.com","https://b.example.com"]`)
	if _, err := store.Set(ctx, "http.cors_origins", value, settings.Change{Version: 0}); err != nil {
		t.Fatal(err)
	}
	// PostgreSQL reformats jsonb; the comparison must be semantic.
	v, err := store.Set(ctx, "http.cors_origins", json.RawMessage(`[ "https://a.example.com", "https://b.example.com" ]`), settings.Change{Version: 1})
	if err != nil || v.Version != 1 {
		t.Errorf("Set(same value) = version %d, %v; want version 1 unchanged", v.Version, err)
	}
	if len(rec.events) != 1 {
		t.Errorf("recorded %d audit events, want 1", len(rec.events))
	}
}

func TestStoredValuesLoadAtStartup(t *testing.T) {
	ctx := operator()
	pool := pgtest.New(t, pgtest.WithMigrations(settings.Migrations))
	first, _ := newStore(t, pool, declare())
	if _, err := first.Set(ctx, "mail.delivery", json.RawMessage(`"sync"`), settings.Change{}); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Set(ctx, "ops.maintenance_mode", json.RawMessage(`true`), settings.Change{}); err != nil {
		t.Fatal(err)
	}
	// A value that no longer passes validation, and a key no longer declared.
	if _, err := pool.Exec(ctx, `UPDATE settings_values SET value = '"carrier-pigeon"' WHERE key = 'mail.delivery'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO settings_values (key, value, version, updated_by) VALUES ('legacy.flag', 'true', 1, 'usr_old')`); err != nil {
		t.Fatal(err)
	}

	d := declare()
	second, _ := newStore(t, pool, d)
	if !d.maintained.Get(ctx) {
		t.Error("stored value was not loaded at startup")
	}
	if got := d.mode.Get(ctx); got != "async" {
		t.Errorf("Get() with an invalid stored value = %q, want the default", got)
	}
	if v := mustGet(t, second, "mail.delivery"); !v.InvalidStoredValue || v.Modified || string(v.Value) != `"async"` {
		t.Errorf("view of invalid stored value = %+v", v)
	}
	if keys := second.UnknownKeys(); len(keys) != 1 || keys[0] != "legacy.flag" {
		t.Errorf("UnknownKeys() = %v, want [legacy.flag]", keys)
	}
}

func TestRestartRequiredSettingsKeepStartupValue(t *testing.T) {
	ctx := operator()
	pool := pgtest.New(t, pgtest.WithMigrations(settings.Migrations))
	d := declare()
	store, _ := newStore(t, pool, d)

	v, err := store.Set(ctx, "jobs.workers", json.RawMessage(`16`), settings.Change{})
	if err != nil {
		t.Fatal(err)
	}
	if got := d.workers.Get(ctx); got != 4 {
		t.Errorf("Get() after changing a restart-required setting = %d, want the startup value 4", got)
	}
	if !v.RestartRequired || !v.RestartPending || string(v.Value) != "16" {
		t.Errorf("view = %+v, want restart pending with the new value", v)
	}

	restarted := declare()
	after, _ := newStore(t, pool, restarted)
	if got := restarted.workers.Get(ctx); got != 16 {
		t.Errorf("Get() after restart = %d, want 16", got)
	}
	if v := mustGet(t, after, "jobs.workers"); v.RestartPending {
		t.Error("RestartPending after restart = true")
	}
}

func TestDeclaringAfterNewStorePanics(t *testing.T) {
	pool := pgtest.New(t, pgtest.WithMigrations(settings.Migrations))
	d := declare()
	newStore(t, pool, d)
	defer func() {
		if msg, _ := recover().(string); !strings.Contains(msg, "declared after NewStore") {
			t.Errorf("panic = %q, want declared after NewStore", msg)
		}
	}()
	settings.Bool(d.reg, "late.flag", false)
}

func TestConcurrentFirstChangesOneWins(t *testing.T) {
	ctx := operator()
	pool := pgtest.New(t, pgtest.WithMigrations(settings.Migrations), pgtest.WithMaxConns(10))
	store, _ := newStore(t, pool, declare())

	const writers = 6
	errs := make([]error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Go(func() {
			value := json.RawMessage(`[]`)
			if i%2 == 0 {
				value = json.RawMessage(`["https://a.example.com"]`)
			}
			_, errs[i] = store.Set(ctx, "http.cors_origins", value, settings.Change{Version: 0})
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
	// Writers setting the value the winner already stored may also succeed as
	// no-ops only if they saw version 1; with version 0 they all conflict.
	if succeeded != 1 {
		t.Errorf("%d concurrent first changes succeeded, want exactly 1", succeeded)
	}
	if v := mustGet(t, store, "http.cors_origins"); v.Version != 1 {
		t.Errorf("version = %d, want 1", v.Version)
	}
}

func TestInstancesConvergeThroughNotifications(t *testing.T) {
	ctx := operator()
	pool := pgtest.New(t, pgtest.WithMigrations(settings.Migrations))
	a, b := declare(), declare()
	storeA, _ := newStore(t, pool, a)
	// A long resync interval: only notifications can deliver the change.
	storeB, _ := newStore(t, pool, b, settings.WithResyncInterval(time.Hour))

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- storeB.Run(runCtx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Run() error = %v", err)
		}
	}()

	// Run reloads after LISTEN starts, so a change made before that is
	// also picked up; wait for B to be listening to test notifications.
	waitFor(t, "listener to start", func() bool {
		var n int
		_ = pool.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE query = 'LISTEN apistock_settings'").Scan(&n)
		return n > 0
	})

	if _, err := storeA.Set(ctx, "ops.maintenance_mode", json.RawMessage(`true`), settings.Change{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "instance B to see the change", func() bool { return b.maintained.Get(ctx) })
	if v := mustGet(t, storeB, "ops.maintenance_mode"); v.Version != 1 || v.UpdatedBy != "usr_ops" {
		t.Errorf("instance B view = %+v", v)
	}
}

func TestPeriodicResyncCatchesChangesWithoutNotifications(t *testing.T) {
	ctx := operator()
	pool := pgtest.New(t, pgtest.WithMigrations(settings.Migrations))
	d := declare()
	store, _ := newStore(t, pool, d, settings.WithResyncInterval(100*time.Millisecond))

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- store.Run(runCtx) }()
	defer func() {
		cancel()
		<-done
	}()

	// A direct SQL change sends no notification.
	if _, err := pool.Exec(ctx, `INSERT INTO settings_values (key, value, version, updated_by) VALUES ('mail.delivery', '"sync"', 1, 'usr_sql')`); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "periodic resync", func() bool { return d.mode.Get(ctx) == "sync" })
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
