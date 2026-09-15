package auditpg_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	"gorbital.dev/modules/auditpg"
)

func TestStats(t *testing.T) {
	store, _ := newStore(t)
	user := actor.With(context.Background(), actor.Actor{Kind: actor.KindUser, ID: "usr_1"})
	t0 := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	record := func(at time.Time, action string, outcome audit.Outcome) {
		t.Helper()
		if err := store.Record(user, audit.Event{OccurredAt: at, Action: action, Outcome: outcome}); err != nil {
			t.Fatal(err)
		}
	}
	for range 3 {
		record(t0, "auth.login.failed", audit.OutcomeFailure)
	}
	record(t0.Add(24*time.Hour), "auth.session.created", audit.OutcomeSuccess)
	record(t0.Add(24*time.Hour+time.Minute), "settings.value.changed", audit.OutcomeSuccess)
	record(t0.Add(-30*24*time.Hour), "auth.login.failed", audit.OutcomeFailure) // outside the window

	window := auditpg.Filter{From: t0.Add(-time.Hour), To: t0.Add(48 * time.Hour)}
	count := func(f auditpg.StatsFilter) auditpg.Stats {
		t.Helper()
		s, err := store.Stats(context.Background(), f)
		if err != nil {
			t.Fatalf("Stats(%+v) error = %v", f, err)
		}
		return s
	}

	byAction := count(auditpg.StatsFilter{Filter: window, GroupBy: auditpg.StatsByAction})
	if got := fmt.Sprint(byAction.Total, byAction.Groups, byAction.Other); got != "5 [{auth.login.failed 3} {auth.session.created 1} {settings.value.changed 1}] 0" {
		t.Errorf("by action = %s", got)
	}
	byDay := count(auditpg.StatsFilter{Filter: window, GroupBy: auditpg.StatsByDay})
	if got := fmt.Sprint(byDay.Groups); got != "[{2026-09-01 3} {2026-09-02 2}]" {
		t.Errorf("by day = %s", got)
	}
	prefix := window
	prefix.ActionPrefix = "auth."
	if got := count(auditpg.StatsFilter{Filter: prefix, GroupBy: auditpg.StatsByOutcome}); fmt.Sprint(got.Total, got.Groups) != "4 [{failure 3} {success 1}]" {
		t.Errorf("auth. by outcome = %v %v", got.Total, got.Groups)
	}
	if got := count(auditpg.StatsFilter{Filter: window, GroupBy: auditpg.StatsByResourceType}); fmt.Sprint(got.Groups) != "[{ 5}]" {
		t.Errorf("by resource type = %v, want one group without a resource type", got.Groups)
	}

	// The default window is the last 7 days: none of these events.
	if got := count(auditpg.StatsFilter{GroupBy: auditpg.StatsByAction}); got.Total != 0 || got.To.Sub(got.From) != auditpg.DefaultStatsWindow || got.Groups == nil {
		t.Errorf("default window = %+v", got)
	}

	for name, f := range map[string]auditpg.StatsFilter{
		"unknown grouping": {Filter: window, GroupBy: "actor_id"},
		"window too wide":  {Filter: auditpg.Filter{From: t0, To: t0.Add(91 * 24 * time.Hour)}, GroupBy: auditpg.StatsByDay},
		"from after to":    {Filter: auditpg.Filter{From: t0, To: t0.Add(-time.Hour)}, GroupBy: auditpg.StatsByDay},
		"bad outcome":      {Filter: auditpg.Filter{Outcome: "maybe"}, GroupBy: auditpg.StatsByAction},
	} {
		if _, err := store.Stats(context.Background(), f); !errors.Is(err, auditpg.ErrInvalidFilter) {
			t.Errorf("%s: error = %v, want ErrInvalidFilter", name, err)
		}
	}
}

func TestStatsCountsGroupsBeyondTheLimit(t *testing.T) {
	store, _ := newStore(t)
	ctx := actor.With(context.Background(), actor.System("test"))
	at := time.Now().Add(-time.Hour)
	for i := range auditpg.MaxStatsGroups + 5 {
		if err := store.Record(ctx, audit.Event{OccurredAt: at, Action: fmt.Sprintf("test.action_%02d", i), Outcome: audit.OutcomeSuccess}); err != nil {
			t.Fatal(err)
		}
	}
	s, err := store.Stats(context.Background(), auditpg.StatsFilter{GroupBy: auditpg.StatsByAction})
	if err != nil {
		t.Fatal(err)
	}
	if s.Total != int64(auditpg.MaxStatsGroups+5) || len(s.Groups) != auditpg.MaxStatsGroups || s.Other != 5 {
		t.Errorf("stats = total %d, %d groups, other %d; want %d, %d, 5", s.Total, len(s.Groups), s.Other, auditpg.MaxStatsGroups+5, auditpg.MaxStatsGroups)
	}
}
