package jobs

import (
	"context"
	"maps"
	"testing"
	"time"

	"github.com/riverqueue/river"

	"gorbital.dev/modules/postgres/pgtest"
)

func TestParseSchedule(t *testing.T) {
	if s, err := parseSchedule(""); err != nil || s != nil {
		t.Errorf("parseSchedule(\"\") = %v, %v; want no schedule", s, err)
	}
	for _, bad := range []string{"@every 59s", "every day", "0 3 * *"} {
		if _, err := parseSchedule(bad); err == nil {
			t.Errorf("parseSchedule(%q) error = nil", bad)
		}
	}
	s, err := parseSchedule("0 3 * * *")
	if err != nil {
		t.Fatal(err)
	}
	// Evaluated in UTC whatever the input's location.
	dubai := time.FixedZone("GST", 4*60*60)
	from := time.Date(2026, 9, 14, 6, 30, 0, 0, dubai) // 02:30 UTC
	if next := s.Next(from); !next.Equal(time.Date(2026, 9, 14, 3, 0, 0, 0, time.UTC)) {
		t.Errorf("Next(%v) = %v, want 03:00 UTC the same day", from, next)
	}
}

type nightlyArgs struct{}

func (nightlyArgs) Kind() string { return "nightly" }

type adhocArgs struct{}

func (adhocArgs) Kind() string { return "adhoc" }

func TestSchedulerFollowsEffectiveConfiguration(t *testing.T) {
	pool := pgtest.New(t)
	if _, err := Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	defs := NewDefinitions()
	Define(defs, Definition[nightlyArgs]{
		Name:     "nightly",
		Worker:   river.WorkFunc(func(context.Context, *river.Job[nightlyArgs]) error { return nil }),
		NewArgs:  func() nightlyArgs { return nightlyArgs{} },
		Enabled:  true,
		Schedule: "0 3 * * *",
	})
	Define(defs, Definition[adhocArgs]{
		Name:    "adhoc",
		Worker:  river.WorkFunc(func(context.Context, *river.Job[adhocArgs]) error { return nil }),
		NewArgs: func() adhocArgs { return adhocArgs{} },
		Enabled: true,
	})
	client, err := New(pool, nil, WithQueues(DefaultQueues()), WithDefinitions(defs))
	if err != nil {
		t.Fatal(err)
	}
	check := func(step string, want map[string]string) {
		t.Helper()
		if err := client.applySchedules(); err != nil {
			t.Fatalf("%s: applySchedules() error = %v", step, err)
		}
		if got := client.sched.applied; !maps.Equal(got, want) {
			t.Errorf("%s: scheduled = %v, want %v", step, got, want)
		}
	}

	check("defaults", map[string]string{"nightly": "0 3 * * *"})

	off := false
	defs.applyOverride("nightly", override{enabled: &off, version: 1})
	check("disabled", map[string]string{})

	on, every := true, "@every 2h"
	defs.applyOverride("nightly", override{enabled: &on, schedule: &every, version: 2})
	check("rescheduled", map[string]string{"nightly": "@every 2h"})

	defs.applyOverride("nightly", override{enabled: &off, version: 1})
	check("older version ignored", map[string]string{"nightly": "@every 2h"})

	hourly := "@hourly"
	defs.applyOverride("adhoc", override{schedule: &hourly, version: 1})
	check("on-demand job given a schedule", map[string]string{"nightly": "@every 2h", "adhoc": "@hourly"})
}
