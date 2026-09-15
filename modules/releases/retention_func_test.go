package releases

import (
	"context"
	"testing"
	"time"
)

// TestRetentionFuncAppliesAtStart: a retention read from a function, such as
// a runtime setting, applies at each start and is clamped to its bounds.
func TestRetentionFuncAppliesAtStart(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	clock := newClock()
	store := must(NewStore(pool, at(clock)))
	retention := 30 * 24 * time.Hour
	fromSetting := WithRetentionFunc(func(context.Context) time.Duration { return retention })

	must(NewTracker(pool, v1, at(clock), host("pod-a"))).start(ctx)
	clock.advance(10 * 24 * time.Hour)
	must(NewTracker(pool, v2, at(clock), host("pod-b"), fromSetting)).start(ctx)
	if n := len(must(store.Instances(ctx, InstanceFilter{})).Instances); n != 2 {
		t.Fatalf("instances within 30 days = %d, want 2", n)
	}

	// An hour is below the 1-day minimum, so a day applies: both earlier
	// instances were last seen longer ago.
	retention = time.Hour
	clock.advance(2 * 24 * time.Hour)
	must(NewTracker(pool, v2, at(clock), host("pod-c"), fromSetting)).start(ctx)
	all := must(store.Instances(ctx, InstanceFilter{})).Instances
	if len(all) != 1 || all[0].Host != "pod-c" {
		t.Errorf("instances after a 1-day retention = %+v, want only pod-c", all)
	}
	if got := (config{retention: DefaultRetention, retentionFunc: func(context.Context) time.Duration { return 10 * 365 * 24 * time.Hour }}).retentionAt(ctx); got != maxRetention {
		t.Errorf("retentionAt(10 years) = %v, want the 3-year maximum", got)
	}
}
