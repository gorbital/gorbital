package releases

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"apistock.dev/buildinfo"
	"apistock.dev/modules/postgres/pgtest"
)

// These tests drive trackers through start, beat and stop with a fake clock
// instead of waiting for heartbeats.

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }
func newClock() *fakeClock                   { return &fakeClock{t: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)} }
func newPool(t *testing.T) *pgxpool.Pool     { return pgtest.New(t, pgtest.WithMigrations(Migrations)) }
func at(c *fakeClock) Option                 { return WithClock(c.now) }
func host(name string) Option                { return WithHost(name) }

// must returns v, and panics when a call the test relies on fails.
func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

var (
	v1 = buildinfo.Info{Version: "v1.0.0", Commit: "aaa111", BuildTime: "2026-09-15T10:00:00Z", GoVersion: "go1.26.1"}
	v2 = buildinfo.Info{Version: "v1.1.0", Commit: "bbb222", BuildTime: "not a time", Modified: true, GoVersion: "go1.26.1"}
)

func TestOptionsValidate(t *testing.T) {
	pool := newPool(t)
	for _, tt := range []struct {
		name string
		opts []Option
	}{
		{"short heartbeat", []Option{WithHeartbeat(time.Second)}},
		{"long heartbeat", []Option{WithHeartbeat(time.Hour)}},
		{"short retention", []Option{WithRetention(time.Hour)}},
		{"long retention", []Option{WithRetention(4 * 365 * 24 * time.Hour)}},
		{"nil clock", []Option{WithClock(nil)}},
	} {
		if _, err := NewTracker(pool, v1, tt.opts...); err == nil {
			t.Errorf("NewTracker(%s) error = nil", tt.name)
		}
		if _, err := NewStore(pool, tt.opts...); err == nil {
			t.Errorf("NewStore(%s) error = nil", tt.name)
		}
	}
	if _, err := NewTracker(nil, v1); err == nil {
		t.Error("NewTracker(nil pool) error = nil")
	}
	if _, err := NewStore(nil); err == nil {
		t.Error("NewStore(nil pool) error = nil")
	}
}

func TestTracksRollingDeploy(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	clock := newClock()
	store := must(NewStore(pool, at(clock)))

	a := must(NewTracker(pool, v1, at(clock), host("pod-a")))
	b := must(NewTracker(pool, v1, at(clock), host("pod-b")))
	a.start(ctx)
	b.start(ctx)

	// A rolling deploy starts v1.1.0 a minute later. pod-b crashes: it sends
	// no more heartbeats and never stops.
	clock.advance(time.Minute)
	c := must(NewTracker(pool, v2, at(clock), host("pod-c")))
	c.start(ctx)
	clock.advance(time.Minute)
	a.beat(ctx)
	c.beat(ctx)

	current := must(store.Current(ctx))
	if len(current) != 2 || current[0].Version != "v1.1.0" || current[1].Version != "v1.0.0" ||
		len(current[1].Instances) != 1 || current[1].Instances[0].Host != "pod-a" || !current[1].Instances[0].Running {
		t.Fatalf("Current() during the deploy = %+v, want v1.1.0 on pod-c and v1.0.0 on pod-a only", current)
	}

	a.stop(ctx)
	current = must(store.Current(ctx))
	if len(current) != 1 || current[0].Version != "v1.1.0" || current[0].Commit != "bbb222" {
		t.Errorf("Current() after pod-a stopped = %+v, want only v1.1.0", current)
	}

	first := must(store.Releases(ctx, ReleaseFilter{Limit: 1}))
	if len(first.Releases) != 1 || first.NextCursor == "" {
		t.Fatalf("Releases(limit 1) = %+v", first)
	}
	newest := first.Releases[0]
	if newest.Version != "v1.1.0" || newest.Running != 1 || newest.Starts != 1 || !newest.Modified {
		t.Errorf("newest release = %+v, want v1.1.0 running once, modified", newest)
	}
	second := must(store.Releases(ctx, ReleaseFilter{Limit: 1, Cursor: first.NextCursor}))
	if len(second.Releases) != 1 || second.NextCursor != "" {
		t.Fatalf("Releases(second page) = %+v", second)
	}
	older := second.Releases[0]
	if older.Version != "v1.0.0" || older.Running != 0 || older.Starts != 2 || older.Modified ||
		!older.FirstStartedAt.Equal(time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)) || !older.LastSeenAt.Equal(clock.t) {
		t.Errorf("older release = %+v, want v1.0.0 with 2 starts and none running", older)
	}
	for _, cursor := range []string{"not a cursor", "e30"} {
		if _, err := store.Releases(ctx, ReleaseFilter{Cursor: cursor}); !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("Releases(cursor %q) error = %v, want ErrInvalidCursor", cursor, err)
		}
	}

	instances := must(store.Instances(ctx, InstanceFilter{Version: "v1.0.0", Commit: "aaa111"}))
	if len(instances.Instances) != 2 {
		t.Fatalf("Instances(v1.0.0) = %+v", instances)
	}
	podB, podA := instances.Instances[0], instances.Instances[1]
	if podB.Host != "pod-b" || podB.Running || podB.StoppedAt != nil || podA.StoppedAt == nil || podA.Running ||
		podA.BuildTime == nil || !podA.BuildTime.Equal(time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)) || podA.GoVersion != "go1.26.1" {
		t.Errorf("v1.0.0 instances = %+v, %+v; want pod-b stale and pod-a stopped", podB, podA)
	}
	running := must(store.Instances(ctx, InstanceFilter{RunningOnly: true}))
	if len(running.Instances) != 1 || running.Instances[0].Host != "pod-c" || running.Instances[0].BuildTime != nil || !running.Instances[0].Modified {
		t.Errorf("Instances(running) = %+v, want pod-c with no build time", running)
	}
	paged := must(store.Instances(ctx, InstanceFilter{Limit: 2}))
	rest := must(store.Instances(ctx, InstanceFilter{Limit: 2, Cursor: paged.NextCursor}))
	if len(paged.Instances) != 2 || paged.NextCursor == "" || len(rest.Instances) != 1 || rest.NextCursor != "" {
		t.Errorf("Instances pages = %d then %d, want 2 then 1", len(paged.Instances), len(rest.Instances))
	}
	if _, err := store.Instances(ctx, InstanceFilter{Cursor: "x"}); !errors.Is(err, ErrInvalidCursor) {
		t.Errorf("Instances(bad cursor) error = %v", err)
	}

	// Starting 91 days later deletes every instance last seen before the
	// retention, stopped or not.
	clock.advance(91 * 24 * time.Hour)
	d := must(NewTracker(pool, buildinfo.Info{}, at(clock), host("pod-d")))
	d.start(ctx)
	all := must(store.Instances(ctx, InstanceFilter{}))
	if len(all.Instances) != 1 || all.Instances[0].Version != "dev" || all.Instances[0].Commit != "" {
		t.Errorf("Instances() after retention = %+v, want only the new dev instance", all.Instances)
	}
}

func TestBeatRecordsAgainWhenRowIsGone(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	clock := newClock()
	tracker := must(NewTracker(pool, v1, at(clock), host("pod-a")))
	tracker.start(ctx)
	first := tracker.id
	if _, err := pool.Exec(ctx, `DELETE FROM release_instances`); err != nil {
		t.Fatal(err)
	}
	clock.advance(DefaultHeartbeat)
	tracker.beat(ctx)
	if tracker.id == 0 || tracker.id == first {
		t.Errorf("tracker row after its row was deleted = %d, want a new row", tracker.id)
	}
}

func TestTrackingFailuresAreLoggedNotFatal(t *testing.T) {
	pool := pgtest.New(t) // no migrations: every write fails
	var logs bytes.Buffer
	tracker := must(NewTracker(pool, v1, WithLogger(slog.New(slog.NewTextHandler(&logs, nil)))))
	ctx := context.Background()
	tracker.start(ctx)
	tracker.beat(ctx)
	tracker.stop(ctx)
	if tracker.id != 0 || strings.Count(logs.String(), "record release instance") != 2 {
		t.Errorf("logs = %s, want two failed records", logs.String())
	}
}

func TestRunRecordsStartAndStop(t *testing.T) {
	pool := newPool(t)
	tracker := must(NewTracker(pool, v1, host("pod-a")))
	store := must(NewStore(pool))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- tracker.Run(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		current := must(store.Current(context.Background()))
		if len(current) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Run() didn't record the instance")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run() = %v", err)
	}
	all := must(store.Instances(context.Background(), InstanceFilter{}))
	if len(all.Instances) != 1 || all.Instances[0].StoppedAt == nil {
		t.Errorf("instance after Run returned = %+v, want stopped", all.Instances)
	}
}
