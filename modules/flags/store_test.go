package flags_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	"gorbital.dev/modules/flags"
	"gorbital.dev/modules/postgres/pgtest"
	"gorbital.dev/requestid"
)

// declared is the flag set used by store tests.
type declared struct {
	reg      *flags.Registry
	checkout *flags.Flag
	search   *flags.Flag
	internal *flags.Flag
}

func declare() declared {
	reg := flags.NewRegistry()
	return declared{
		reg: reg,
		checkout: flags.Bool(reg, "checkout.new_flow",
			flags.Describe("The redesigned checkout."),
			flags.Client(),
		),
		search:   flags.Bool(reg, "search.enabled", flags.DefaultOn(), flags.Client(), flags.Group("discovery")),
		internal: flags.Bool(reg, "billing.new_invoices"),
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

func (r *recorded) list() []audit.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]audit.Event(nil), r.events...)
}

func newStore(t *testing.T, pool *pgxpool.Pool, d declared, opts ...flags.StoreOption) (*flags.Store, *recorded) {
	t.Helper()
	rec := &recorded{}
	store, err := flags.NewStore(context.Background(), pool, d.reg, rec, opts...)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	return store, rec
}

func operator() context.Context {
	ctx := actor.With(context.Background(), actor.Actor{Kind: actor.KindUser, ID: "usr_ops", Label: "Ops"})
	return requestid.With(ctx, "req_123")
}

func user(id string) context.Context {
	return actor.With(context.Background(), actor.Actor{Kind: actor.KindUser, ID: id})
}

func mustGet(t *testing.T, store *flags.Store, key string) flags.View {
	t.Helper()
	v, err := store.Get(key)
	if err != nil {
		t.Fatalf("Get(%s) error = %v", key, err)
	}
	return v
}

func TestNewStoreValidatesDependencies(t *testing.T) {
	_, err := flags.NewStore(context.Background(), nil, nil, nil, flags.WithResyncInterval(0))
	if err == nil {
		t.Fatal("NewStore() error = nil")
	}
	for _, want := range []string{"pool is required", "registry is required", "audit recorder is required", "resync interval"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("NewStore() error = %q, want it to mention %q", err, want)
		}
	}
}

func TestSetResetHistoryAndAudit(t *testing.T) {
	ctx := operator()
	pool := pgtest.New(t, pgtest.WithMigrations(flags.Migrations))
	d := declare()
	store, rec := newStore(t, pool, d)

	list := store.List()
	if len(list) != 3 || list[0].Key != "checkout.new_flow" || list[1].Group != "discovery" || list[2].Group != "billing" {
		t.Fatalf("List() = %+v, want 3 flags in declaration order with groups", list)
	}
	if v := list[0]; v.Modified || v.Version != 0 || v.State.Enabled || !v.Client || v.Description != "The redesigned checkout." {
		t.Errorf("initial view = %+v", v)
	}
	if v := list[1]; !v.State.Enabled || !v.State.Default || !v.Default.Enabled {
		t.Errorf("DefaultOn view = %+v, want enabled with a default of true", v)
	}

	state := flags.State{
		Enabled:    true,
		Users:      flags.Targets{Allow: []string{"usr_b", "usr_a", "usr_a"}},
		Percentage: flags.Percent(0),
	}
	v, err := store.Set(ctx, "checkout.new_flow", state, flags.Change{Version: 0, Reason: "beta testers"})
	if err != nil {
		t.Fatalf("Set() error = %v", err)
	}
	if !v.Modified || v.Version != 1 || !v.State.Enabled || v.UpdatedBy != "usr_ops" ||
		strings.Join(v.State.Users.Allow, ",") != "usr_a,usr_b" || *v.State.Percentage != 0 {
		t.Errorf("view after Set = %+v", v)
	}
	if !d.checkout.Enabled(user("usr_a")) || d.checkout.Enabled(user("usr_c")) {
		t.Error("after Set, the flag isn't on for exactly the allowed user")
	}
	// A view is a copy.
	v.State.Users.Allow[0] = "usr_changed"
	if mustGet(t, store, "checkout.new_flow").State.Users.Allow[0] != "usr_a" {
		t.Error("changing a view changed the store")
	}

	if _, err := store.Set(ctx, "checkout.new_flow", flags.State{}, flags.Change{Version: 0, Reason: "stale"}); !errors.Is(err, flags.ErrVersionConflict) {
		t.Errorf("Set() with a stale version error = %v, want ErrVersionConflict", err)
	}

	v, err = store.Reset(ctx, "checkout.new_flow", flags.Change{Version: 1, Reason: "beta over"})
	if err != nil {
		t.Fatalf("Reset() error = %v", err)
	}
	if v.Modified || v.Version != 2 || v.State.Enabled {
		t.Errorf("view after Reset = %+v", v)
	}
	if d.checkout.Enabled(user("usr_a")) {
		t.Error("after Reset, the flag is still on")
	}
	// Resetting a flag already at its declared state changes nothing.
	if v, err := store.Reset(ctx, "checkout.new_flow", flags.Change{Version: 2, Reason: "again"}); err != nil || v.Version != 2 {
		t.Errorf("second Reset() = version %d, %v; want no change", v.Version, err)
	}

	history, err := store.History(ctx, "checkout.new_flow", 0, 10)
	if err != nil || len(history) != 2 {
		t.Fatalf("History() = %d entries, %v; want 2", len(history), err)
	}
	reset, set := history[0], history[1]
	if reset.Version != 2 || reset.OldState == nil || !reset.OldState.Enabled || reset.NewState != nil || reset.Reason != "beta over" {
		t.Errorf("reset entry = %+v", reset)
	}
	if set.Version != 1 || set.OldState != nil || set.NewState == nil || len(set.NewState.Users.Allow) != 2 ||
		set.ActorKind != actor.KindUser || set.ActorID != "usr_ops" || set.RequestID != "req_123" || set.ChangedAt.IsZero() {
		t.Errorf("set entry = %+v", set)
	}
	page, err := store.History(ctx, "checkout.new_flow", reset.ID, 1)
	if err != nil || len(page) != 1 || page[0].ID != set.ID {
		t.Errorf("History(before=%d, limit=1) = %+v, %v; want the older entry", reset.ID, page, err)
	}
	if _, err := store.History(ctx, "checkout.unknown", 0, 10); !errors.Is(err, flags.ErrUnknownFlag) {
		t.Errorf("History(unknown) error = %v, want ErrUnknownFlag", err)
	}

	events := rec.list()
	if len(events) != 2 {
		t.Fatalf("recorded %d audit events, want 2", len(events))
	}
	if e := events[0]; e.Action != flags.ActionChanged || e.ResourceType != "flag" || e.ResourceID != "checkout.new_flow" ||
		e.Outcome != audit.OutcomeSuccess || e.Metadata["version"] != int64(1) || e.Metadata["reason"] != "beta testers" ||
		e.Metadata["enabled"] != true || e.Metadata["user_targets"] != 2 || e.Metadata["percentage"] != 0 {
		t.Errorf("change audit event = %+v", e)
	}
	if e := events[1]; e.Action != flags.ActionReset || e.Metadata["version"] != int64(2) || e.Metadata["reason"] != "beta over" {
		t.Errorf("reset audit event = %+v", e)
	}
	for _, e := range events {
		if err := e.Validate(); err != nil {
			t.Errorf("audit event is invalid: %v", err)
		}
		if strings.Contains(fmt.Sprint(e.Metadata), "usr_a") {
			t.Errorf("audit event metadata %v lists targeted IDs", e.Metadata)
		}
	}
}

func TestSetRejectsInvalidChanges(t *testing.T) {
	ctx := operator()
	pool := pgtest.New(t, pgtest.WithMigrations(flags.Migrations))
	store, rec := newStore(t, pool, declare())

	tooMany := make([]string, flags.MaxTargets+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("org_%d", i)
	}
	for _, state := range []flags.State{
		{Enabled: true, Percentage: flags.Percent(101)},
		{Enabled: true, Percentage: flags.Percent(-5)},
		{Enabled: true, Orgs: flags.Targets{Allow: tooMany}},
		{Enabled: true, Users: flags.Targets{Deny: []string{"usr secret"}}},
		{Enabled: true, Users: flags.Targets{Allow: []string{"usr_secret"}, Deny: []string{"usr_secret"}}},
	} {
		_, err := store.Set(ctx, "checkout.new_flow", state, flags.Change{Reason: "test"})
		var invalid *flags.InvalidStateError
		if !errors.As(err, &invalid) || !errors.Is(err, flags.ErrInvalidState) || invalid.Key != "checkout.new_flow" {
			t.Errorf("Set(%+v) error = %v, want InvalidStateError", state, err)
			continue
		}
		if strings.Contains(err.Error(), "secret") {
			t.Errorf("Set() error %q echoes an ID", err)
		}
	}

	valid := flags.State{Enabled: true}
	for _, reason := range []string{"", "   "} {
		if _, err := store.Set(ctx, "checkout.new_flow", valid, flags.Change{Reason: reason}); !errors.Is(err, flags.ErrReasonRequired) {
			t.Errorf("Set() with reason %q error = %v, want ErrReasonRequired", reason, err)
		}
	}
	if _, err := store.Reset(ctx, "checkout.new_flow", flags.Change{}); !errors.Is(err, flags.ErrReasonRequired) {
		t.Errorf("Reset() without reason error = %v, want ErrReasonRequired", err)
	}
	if _, err := store.Set(context.Background(), "checkout.new_flow", valid, flags.Change{Reason: "x"}); !errors.Is(err, flags.ErrActorRequired) {
		t.Errorf("Set() without actor error = %v, want ErrActorRequired", err)
	}
	anon := actor.With(context.Background(), actor.Anonymous)
	if _, err := store.Set(anon, "checkout.new_flow", valid, flags.Change{Reason: "x"}); !errors.Is(err, flags.ErrActorRequired) {
		t.Errorf("Set() as anonymous error = %v, want ErrActorRequired", err)
	}
	if _, err := store.Set(ctx, "checkout.unknown", valid, flags.Change{Reason: "x"}); !errors.Is(err, flags.ErrUnknownFlag) {
		t.Errorf("Set(unknown) error = %v, want ErrUnknownFlag", err)
	}
	if _, err := store.Get("checkout.unknown"); !errors.Is(err, flags.ErrUnknownFlag) {
		t.Errorf("Get(unknown) error = %v, want ErrUnknownFlag", err)
	}

	for _, v := range store.List() {
		if v.Version != 0 {
			t.Errorf("%s changed to version %d by a rejected change", v.Key, v.Version)
		}
	}
	if n := len(rec.list()); n != 0 {
		t.Errorf("rejected changes recorded %d audit events", n)
	}
	var rows int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM flags_states) + (SELECT count(*) FROM flags_history)`).Scan(&rows); err != nil || rows != 0 {
		t.Errorf("rejected changes left %d rows (%v)", rows, err)
	}
}

func TestSettingTheSameStateIsANoOp(t *testing.T) {
	ctx := operator()
	pool := pgtest.New(t, pgtest.WithMigrations(flags.Migrations))
	store, rec := newStore(t, pool, declare())

	first := flags.State{Enabled: true, Orgs: flags.Targets{Allow: []string{"org_b", "org_a"}}}
	if _, err := store.Set(ctx, "checkout.new_flow", first, flags.Change{Reason: "x"}); err != nil {
		t.Fatal(err)
	}
	// The same lists in another order are the same state.
	same := flags.State{Enabled: true, Orgs: flags.Targets{Allow: []string{"org_a", "org_b", "org_a"}}}
	if v, err := store.Set(ctx, "checkout.new_flow", same, flags.Change{Version: 1, Reason: "again"}); err != nil || v.Version != 1 {
		t.Errorf("Set(same state) = version %d, %v; want version 1 unchanged", v.Version, err)
	}
	if n := len(rec.list()); n != 1 {
		t.Errorf("recorded %d audit events, want 1", n)
	}
}

func TestClientFlags(t *testing.T) {
	ctx := operator()
	pool := pgtest.New(t, pgtest.WithMigrations(flags.Migrations))
	d := declare()
	store, _ := newStore(t, pool, d)
	// The server-side flag is on for everyone; it must still not be listed.
	if _, err := store.Set(ctx, "billing.new_invoices", flags.State{Enabled: true, Default: true}, flags.Change{Reason: "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Set(ctx, "checkout.new_flow", flags.State{Enabled: true, Users: flags.Targets{Allow: []string{"usr_a"}}}, flags.Change{Reason: "x"}); err != nil {
		t.Fatal(err)
	}

	for id, want := range map[string]string{"usr_a": "checkout.new_flow=true search.enabled=true", "usr_b": "checkout.new_flow=false search.enabled=true"} {
		var got []string
		for _, e := range store.ClientFlags(user(id)) {
			got = append(got, fmt.Sprintf("%s=%v", e.Key, e.Enabled))
		}
		if strings.Join(got, " ") != want {
			t.Errorf("ClientFlags(%s) = %v, want %s", id, got, want)
		}
	}
}

func TestStoredStatesLoadAtStartup(t *testing.T) {
	ctx := operator()
	pool := pgtest.New(t, pgtest.WithMigrations(flags.Migrations))
	first, _ := newStore(t, pool, declare())
	if _, err := first.Set(ctx, "checkout.new_flow", flags.State{Enabled: true, Default: true}, flags.Change{Reason: "launch"}); err != nil {
		t.Fatal(err)
	}
	if _, err := first.Set(ctx, "search.enabled", flags.State{Enabled: false}, flags.Change{Reason: "incident"}); err != nil {
		t.Fatal(err)
	}
	// A state no longer within the bounds, an unknown field from a later
	// release, and a key no longer declared.
	if _, err := pool.Exec(ctx, `UPDATE flags_states SET state = '{"enabled": true, "default": true, "percentage": 150}' WHERE key = 'search.enabled'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE flags_states SET state = state || '{"schedule": "later"}' WHERE key = 'checkout.new_flow'`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO flags_states (key, state, version, updated_by) VALUES ('legacy.flag', '{"enabled": true}', 1, 'usr_old')`); err != nil {
		t.Fatal(err)
	}

	d := declare()
	second, _ := newStore(t, pool, d)
	if !d.checkout.Enabled(user("usr_a")) {
		t.Error("stored state was not loaded at startup")
	}
	if v := mustGet(t, second, "search.enabled"); !v.InvalidStoredValue || v.Modified || !v.State.Enabled || v.Version != 1 {
		t.Errorf("view of an invalid stored state = %+v, want the declared state flagged", v)
	}
	if !d.search.Enabled(user("usr_a")) {
		t.Error("a flag with an invalid stored state doesn't follow its declared state")
	}
	if keys := second.UnknownKeys(); len(keys) != 1 || keys[0] != "legacy.flag" {
		t.Errorf("UnknownKeys() = %v, want [legacy.flag]", keys)
	}
	history, err := second.History(ctx, "search.enabled", 0, 10)
	if err != nil || len(history) != 1 || history[0].NewState == nil || history[0].NewState.Enabled {
		t.Errorf("History() = %+v, %v", history, err)
	}
}

func TestDeclaringAfterNewStorePanics(t *testing.T) {
	pool := pgtest.New(t, pgtest.WithMigrations(flags.Migrations))
	d := declare()
	newStore(t, pool, d)
	defer func() {
		if msg, _ := recover().(string); !strings.Contains(msg, "declared after NewStore") {
			t.Errorf("panic = %q, want declared after NewStore", msg)
		}
	}()
	flags.Bool(d.reg, "late.flag")
}

func TestConcurrentFirstChangesOneWins(t *testing.T) {
	ctx := operator()
	pool := pgtest.New(t, pgtest.WithMigrations(flags.Migrations), pgtest.WithMaxConns(10))
	store, _ := newStore(t, pool, declare())

	const writers = 6
	errs := make([]error, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Go(func() {
			state := flags.State{Enabled: true, Percentage: flags.Percent(i * 10)}
			_, errs[i] = store.Set(ctx, "checkout.new_flow", state, flags.Change{Version: 0, Reason: "race"})
		})
	}
	wg.Wait()

	succeeded := 0
	for i, err := range errs {
		switch {
		case err == nil:
			succeeded++
		case !errors.Is(err, flags.ErrVersionConflict):
			t.Errorf("writer %d: error = %v, want nil or ErrVersionConflict", i, err)
		}
	}
	if succeeded != 1 {
		t.Errorf("%d concurrent first changes succeeded, want exactly 1", succeeded)
	}
	if v := mustGet(t, store, "checkout.new_flow"); v.Version != 1 {
		t.Errorf("version = %d, want 1", v.Version)
	}
}

func TestInstancesConvergeThroughNotifications(t *testing.T) {
	ctx := operator()
	pool := pgtest.New(t, pgtest.WithMigrations(flags.Migrations))
	a, b := declare(), declare()
	storeA, _ := newStore(t, pool, a)
	// A long resync interval: only notifications can deliver the change.
	storeB, _ := newStore(t, pool, b, flags.WithResyncInterval(time.Hour))

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
		_ = pool.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE query = 'LISTEN gorbital_flags'").Scan(&n)
		return n > 0
	})

	state := flags.State{Enabled: true, Percentage: flags.Percent(100)}
	if _, err := storeA.Set(ctx, "checkout.new_flow", state, flags.Change{Reason: "launch"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "instance B to see the change", func() bool { return b.checkout.Enabled(user("usr_a")) })
	if v := mustGet(t, storeB, "checkout.new_flow"); v.Version != 1 || v.UpdatedBy != "usr_ops" {
		t.Errorf("instance B view = %+v", v)
	}

	if _, err := storeA.Reset(ctx, "checkout.new_flow", flags.Change{Version: 1, Reason: "rollback"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "instance B to see the reset", func() bool { return !b.checkout.Enabled(user("usr_a")) })
}

func TestPeriodicResyncCatchesChangesWithoutNotifications(t *testing.T) {
	ctx := operator()
	pool := pgtest.New(t, pgtest.WithMigrations(flags.Migrations))
	d := declare()
	store, _ := newStore(t, pool, d, flags.WithResyncInterval(100*time.Millisecond))

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- store.Run(runCtx) }()
	defer func() {
		cancel()
		<-done
	}()

	// A direct SQL change sends no notification.
	if _, err := pool.Exec(ctx, `INSERT INTO flags_states (key, state, version, updated_by) VALUES ('checkout.new_flow', '{"enabled": true, "default": true}', 1, 'usr_sql')`); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "periodic resync", func() bool { return d.checkout.Enabled(user("usr_a")) })
}

func TestDeleteHistoryBefore(t *testing.T) {
	ctx := operator()
	pool := pgtest.New(t, pgtest.WithMigrations(flags.Migrations))
	store, _ := newStore(t, pool, declare())
	if _, ok, err := store.OldestHistory(ctx); ok || err != nil {
		t.Errorf("OldestHistory() with no changes = %v, %v", ok, err)
	}
	for i := range 3 {
		if _, err := store.Set(ctx, "checkout.new_flow", flags.State{Enabled: true, Percentage: flags.Percent(i)}, flags.Change{Version: int64(i), Reason: "step"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE flags_history SET changed_at = now() - interval '2 days' WHERE version < 3`); err != nil {
		t.Fatal(err)
	}
	if oldest, ok, err := store.OldestHistory(ctx); !ok || err != nil || time.Since(oldest) < 47*time.Hour {
		t.Errorf("OldestHistory() = %v, %v, %v", oldest, ok, err)
	}
	if n, err := store.DeleteHistoryBefore(ctx, time.Now().Add(-24*time.Hour), 1); n != 1 || err != nil {
		t.Errorf("DeleteHistoryBefore(limit 1) = %d, %v; want 1", n, err)
	}
	if n, err := store.DeleteHistoryBefore(ctx, time.Now().Add(-24*time.Hour), 10); n != 1 || err != nil {
		t.Errorf("DeleteHistoryBefore() = %d, %v; want the other old change", n, err)
	}
	if history, _ := store.History(ctx, "checkout.new_flow", 0, 10); len(history) != 1 || history[0].Version != 3 {
		t.Errorf("History() after deleting = %+v, want the recent change", history)
	}
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
