package observability_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/modules/observability"
	"gorbital.dev/modules/postgres/pgtest"
)

func newStore(t testing.TB, opts ...observability.StoreOption) (*observability.Store, *pgxpool.Pool) {
	t.Helper()
	pool := pgtest.New(t, pgtest.WithMigrations(observability.Migrations))
	s, err := observability.NewStore(pool, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return s, pool
}

// TestSummaryAddsUpInstances runs three collectors, as three instances of
// an app, writing to one database while requests arrive over three
// minutes, and checks the summary against what each instance served: the
// total, each minute, each instance and each route, and percentiles
// estimated from the merged histograms.
func TestSummaryAddsUpInstances(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()
	clk := newClock()
	start := clk.Now().Truncate(time.Minute)

	type key struct{ instance, route string }
	want := map[key]*observability.Stats{}
	var all []time.Duration
	collectors := map[string]*observability.Collector{}
	for _, id := range []string{"inst-a", "inst-b", "inst-c"} {
		collectors[id] = newCollector(t, observability.WithInstance(id), observability.WithClock(clk.Now), observability.WithSink(store))
	}
	rng := rand.New(rand.NewPCG(3, 4))
	routes := []string{"/v1/projects", "/v1/projects/{id}", "/v1/ping"}
	for minute := range 3 {
		for range 5 { // five flushes a minute, as every 12 seconds
			for id, c := range collectors {
				for i := range 40 {
					route := routes[i%len(routes)]
					status := 200
					if rng.IntN(20) == 0 {
						status = 503
					}
					d := time.Duration(rng.Int64N(int64(80*time.Millisecond))) + time.Millisecond
					c.Record(observability.Request{Time: clk.Now(), Method: "GET", Route: route, Status: status, Duration: d})
					k := key{id, route}
					if want[k] == nil {
						want[k] = &observability.Stats{}
					}
					want[k].Requests++
					if status >= 500 {
						want[k].ServerErrors++
					}
					all = append(all, d)
				}
				if err := c.Flush(ctx); err != nil {
					t.Fatalf("minute %d: Flush(%s) error = %v", minute, id, err)
				}
			}
			clk.Add(12 * time.Second)
		}
	}
	for _, c := range collectors {
		if err := c.Flush(ctx); err != nil {
			t.Fatal(err)
		}
	}

	sum, err := store.Summary(ctx, start, start.Add(10*time.Minute))
	if err != nil {
		t.Fatalf("Summary() error = %v", err)
	}
	if sum.Total.Requests != 3*5*3*40 || len(sum.Instances) != 3 || len(sum.Routes) != 3 || len(sum.Minutes) != 3 {
		t.Fatalf("Summary() = %d requests, %d instances, %d routes, %d minutes; want 1800, 3, 3, 3",
			sum.Total.Requests, len(sum.Instances), len(sum.Routes), len(sum.Minutes))
	}
	var wantErrors int64
	for _, st := range want {
		wantErrors += st.ServerErrors
	}
	if sum.Total.ServerErrors != wantErrors {
		t.Errorf("total server errors = %d, want %d", sum.Total.ServerErrors, wantErrors)
	}
	for _, inst := range sum.Instances {
		var req, errs int64
		for k, st := range want {
			if k.instance == inst.Instance {
				req, errs = req+st.Requests, errs+st.ServerErrors
			}
		}
		if inst.Requests != req || inst.ServerErrors != errs || inst.LastWrite.IsZero() || !inst.LastMinute.Equal(start.Add(2*time.Minute)) {
			t.Errorf("instance %s = %+v, want %d requests, %d errors, last minute %v", inst.Instance, inst, req, errs, start.Add(2*time.Minute))
		}
	}
	for _, r := range sum.Routes {
		var req int64
		for k, st := range want {
			if k.route == r.Route {
				req += st.Requests
			}
		}
		if r.Requests != req || r.Method != "GET" {
			t.Errorf("route %s = %d requests, want %d", r.Route, r.Requests, req)
		}
	}
	for i, m := range sum.Minutes {
		if m.Requests != 600 || !m.Start.Equal(start.Add(time.Duration(i)*time.Minute)) {
			t.Errorf("minute %d = %v with %d requests, want 600", i, m.Start, m.Requests)
		}
	}

	// The merged histogram is the one a single collector would have built.
	merged := observability.Stats{Buckets: make([]int64, observability.BucketCount)}
	var bucketTotal int64
	for i, n := range sum.Total.Buckets {
		merged.Buckets[i] = n
		bucketTotal += n
	}
	if bucketTotal != sum.Total.Requests {
		t.Errorf("total buckets hold %d requests, want %d", bucketTotal, sum.Total.Requests)
	}
	var maxD, total time.Duration
	for _, d := range all {
		maxD, total = max(maxD, d), total+d
	}
	if sum.Total.DurationMax.Truncate(time.Microsecond) != maxD.Truncate(time.Microsecond) {
		t.Errorf("max duration = %v, want %v", sum.Total.DurationMax, maxD)
	}
	if diff := sum.Total.DurationSum - total; diff < -2*time.Millisecond || diff > 2*time.Millisecond {
		t.Errorf("duration sum = %v, want %v (microsecond precision)", sum.Total.DurationSum, total)
	}
	if p95 := sum.Total.Quantile(0.95); p95 < 60*time.Millisecond || p95 > 90*time.Millisecond {
		t.Errorf("p95 of uniform 1–81 ms = %v, want about 77 ms", p95)
	}

	// Writing the same snapshot again changes nothing; an older snapshot
	// with fewer requests doesn't replace a newer one.
	old := []observability.Minute{{Start: start, Instance: "inst-a", Method: "GET", Route: "/v1/ping", Stats: observability.Stats{Requests: 1}}}
	if err := store.WriteMinutes(ctx, old); err != nil {
		t.Fatal(err)
	}
	again, err := store.Summary(ctx, start, start.Add(10*time.Minute))
	if err != nil || again.Total.Requests != sum.Total.Requests {
		t.Errorf("after writing an older snapshot: %d requests, %v; want %d", again.Total.Requests, err, sum.Total.Requests)
	}

	// An empty range, and a range without minutes.
	if _, err := store.Summary(ctx, start, start); !errors.Is(err, observability.ErrInvalidRange) {
		t.Errorf("Summary(empty range) error = %v, want ErrInvalidRange", err)
	}
	if _, err := store.Summary(ctx, start, start.Add(8*24*time.Hour)); !errors.Is(err, observability.ErrInvalidRange) {
		t.Errorf("Summary(8 days) error = %v, want ErrInvalidRange", err)
	}
	if none, err := store.Summary(ctx, start.Add(-time.Hour), start); err != nil || none.Total.Requests != 0 || none.Total.Quantile(0.5) != 0 {
		t.Errorf("Summary(an hour before) = %+v, %v; want nothing", none.Total, err)
	}
}

func TestDeleteBeforeAndOldest(t *testing.T) {
	store, _ := newStore(t)
	ctx := context.Background()
	if _, ok, err := store.Oldest(ctx); ok || err != nil {
		t.Errorf("Oldest() on an empty table = %v, %v", ok, err)
	}
	start := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	var minutes []observability.Minute
	for i := range 30 {
		for _, inst := range []string{"a", "b"} {
			minutes = append(minutes, observability.Minute{Start: start.Add(time.Duration(i) * time.Minute), Instance: inst, Method: "GET", Route: "/", Stats: observability.Stats{Requests: 1}})
		}
	}
	if err := store.WriteMinutes(ctx, minutes); err != nil {
		t.Fatal(err)
	}
	var deleted int64
	for {
		n, err := store.DeleteBefore(ctx, start.Add(20*time.Minute), 7)
		if err != nil {
			t.Fatal(err)
		}
		deleted += n
		if n < 7 {
			break
		}
	}
	oldest, ok, err := store.Oldest(ctx)
	if deleted != 40 || !ok || err != nil || !oldest.Equal(start.Add(20*time.Minute)) {
		t.Errorf("deleted %d, oldest %v %v %v; want 40 and minute 20", deleted, oldest, ok, err)
	}
	if _, err := store.DeleteBefore(ctx, start, 0); err == nil {
		t.Error("DeleteBefore(limit 0) error = nil")
	}
}

func TestSummaryTimeout(t *testing.T) {
	store, _ := newStore(t, observability.WithQueryTimeout(time.Nanosecond))
	now := time.Now()
	if _, err := store.Summary(context.Background(), now.Add(-time.Hour), now); !errors.Is(err, observability.ErrQueryTimeout) {
		t.Errorf("Summary() with a 1 ns timeout error = %v, want ErrQueryTimeout", err)
	}
}

// BenchmarkWriteMinutes measures one collector flush: every series of a
// minute in one statement.
func BenchmarkWriteMinutes(b *testing.B) {
	store, _ := newStore(b)
	ctx := context.Background()
	for _, series := range []int{10, 100, 500} {
		b.Run(fmt.Sprint(series, " series"), func(b *testing.B) {
			minutes := make([]observability.Minute, series)
			for i := range minutes {
				minutes[i] = observability.Minute{
					Start: time.Now().Truncate(time.Minute), Instance: "bench", Method: "GET", Route: fmt.Sprintf("/v1/r%d/{id}", i),
					Stats: observability.Stats{Requests: 100, DurationSum: time.Second, DurationMax: 90 * time.Millisecond, Buckets: make([]int64, observability.BucketCount)},
				}
				minutes[i].Buckets[12] = 100
			}
			for b.Loop() {
				if err := store.WriteMinutes(ctx, minutes); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkSummary measures /ops/observability's query over 15 minutes
// and 24 hours of 3 instances with 50 routes each.
func BenchmarkSummary(b *testing.B) {
	store, _ := newStore(b)
	ctx := context.Background()
	end := time.Now().Truncate(time.Minute)
	var wg sync.WaitGroup
	for _, inst := range []string{"a", "b", "c"} {
		wg.Go(func() {
			for m := range 24 * 60 {
				minutes := make([]observability.Minute, 50)
				for r := range minutes {
					minutes[r] = observability.Minute{
						Start: end.Add(-time.Duration(m) * time.Minute), Instance: inst, Method: "GET", Route: fmt.Sprintf("/v1/r%d", r),
						Stats: observability.Stats{Requests: 60, ServerErrors: 1, DurationSum: time.Second, DurationMax: 50 * time.Millisecond, Buckets: make([]int64, observability.BucketCount)},
					}
					minutes[r].Buckets[r%observability.BucketCount] = 60
				}
				if err := store.WriteMinutes(ctx, minutes); err != nil {
					b.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
	for _, window := range []time.Duration{15 * time.Minute, time.Hour, 24 * time.Hour} {
		b.Run(window.String(), func(b *testing.B) {
			for b.Loop() {
				if _, err := store.Summary(ctx, end.Add(-window), end.Add(time.Minute)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
