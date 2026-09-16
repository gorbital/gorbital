package observability

import (
	"math"
	"math/rand/v2"
	"slices"
	"sort"
	"testing"
	"time"
)

func TestBucketOf(t *testing.T) {
	for _, tt := range []struct {
		d    time.Duration
		want int
	}{
		{0, 0},
		{250 * time.Microsecond, 0}, // bounds are inclusive
		{250*time.Microsecond + 1, 1},
		{time.Millisecond, 2},
		{7 * time.Millisecond, 8},
		{time.Minute, len(bucketBounds) - 1},
		{time.Minute + 1, len(bucketBounds)},
		{time.Hour, BucketCount - 1},
	} {
		if got := bucketOf(tt.d); got != tt.want {
			t.Errorf("bucketOf(%v) = %d, want %d", tt.d, got, tt.want)
		}
	}
	if !slices.IsSorted(bucketBounds[:]) {
		t.Error("bucket bounds aren't sorted")
	}
	// Between 1 ms and 10 s, each bound is at most 1.5 times the previous
	// one: the documented error bound of Quantile.
	for i := 1; i < len(bucketBounds); i++ {
		lo, hi := bucketBounds[i-1], bucketBounds[i]
		if lo >= time.Millisecond && hi <= 10*time.Second && float64(hi) > 1.5*float64(lo) {
			t.Errorf("bounds %v to %v grow by more than 1.5", lo, hi)
		}
	}
}

func statsOf(durations []time.Duration) Stats {
	s := Stats{Buckets: make([]int64, BucketCount)}
	for _, d := range durations {
		s.Requests++
		s.DurationSum += d
		s.DurationMax = max(s.DurationMax, d)
		s.Buckets[bucketOf(d)]++
	}
	return s
}

// exactQuantile is the nearest-rank quantile of sorted durations.
func exactQuantile(sorted []time.Duration, q float64) time.Duration {
	rank := int(math.Ceil(q * float64(len(sorted))))
	return sorted[max(rank, 1)-1]
}

// bucketWidth returns the width of the bucket holding d, taking the last
// bucket up to hi.
func bucketWidth(d, hi time.Duration) time.Duration {
	i := bucketOf(d)
	var lo time.Duration
	if i > 0 {
		lo = bucketBounds[i-1]
	}
	if i < len(bucketBounds) {
		hi = bucketBounds[i]
	}
	return hi - lo
}

// TestQuantileError checks Quantile against exact percentiles of sampled
// latency distributions: every estimate lies within the width of the
// bucket holding the exact value (the documented bound), and the typical
// error is logged for ADR-0064.
func TestQuantileError(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	distributions := map[string]func() time.Duration{
		// A typical API: median 20 ms, a long tail.
		"lognormal 20ms": func() time.Duration {
			return time.Duration(math.Exp(math.Log(20e6)+0.8*rng.NormFloat64())) * time.Nanosecond
		},
		"uniform 1-300ms": func() time.Duration {
			return time.Millisecond + time.Duration(rng.Int64N(int64(299*time.Millisecond)))
		},
		// Fast cached reads with a slow database path.
		"bimodal 0.3ms/80ms": func() time.Duration {
			if rng.IntN(10) < 8 {
				return 200*time.Microsecond + time.Duration(rng.Int64N(int64(200*time.Microsecond)))
			}
			return time.Duration(math.Exp(math.Log(80e6)+0.3*rng.NormFloat64())) * time.Nanosecond
		},
		"slow 5-40s": func() time.Duration {
			return 5*time.Second + time.Duration(rng.Int64N(int64(35*time.Second)))
		},
	}
	names := make([]string, 0, len(distributions))
	for name := range distributions {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		sample := distributions[name]
		for _, n := range []int{100, 10_000} {
			durations := make([]time.Duration, n)
			for i := range durations {
				durations[i] = sample()
			}
			s := statsOf(durations)
			slices.Sort(durations)
			var worst float64
			for _, q := range []float64{0.5, 0.9, 0.95, 0.99} {
				exact, est := exactQuantile(durations, q), s.Quantile(q)
				diff := est - exact
				if diff < 0 {
					diff = -diff
				}
				if limit := bucketWidth(exact, s.DurationMax); diff > limit {
					t.Errorf("%s, n=%d: p%v = %v, exact %v: error %v above the bucket width %v", name, n, q*100, est, exact, diff, limit)
				}
				worst = max(worst, float64(diff)/float64(exact))
			}
			t.Logf("%s, n=%d: worst relative error of p50/p90/p95/p99 %.1f%%", name, n, worst*100)
		}
	}
}

func TestStatsWithoutRequests(t *testing.T) {
	var s Stats
	if s.Quantile(0.5) != 0 || s.ErrorRate() != 0 || s.Mean() != 0 {
		t.Errorf("empty stats: p50 %v, error rate %v, mean %v; want zeros", s.Quantile(0.5), s.ErrorRate(), s.Mean())
	}
	one := statsOf([]time.Duration{42 * time.Millisecond})
	one.ServerErrors = 1
	if got := one.Quantile(0.99); got <= 40*time.Millisecond || got > 42*time.Millisecond {
		t.Errorf("one request of 42ms: p99 = %v, want in (40ms, 42ms]", got)
	}
	if one.ErrorRate() != 1 || one.Mean() != 42*time.Millisecond {
		t.Errorf("one failed request: error rate %v, mean %v", one.ErrorRate(), one.Mean())
	}
	if got := one.Quantile(math.NaN()); got != 0 {
		t.Errorf("Quantile(NaN) = %v, want 0", got)
	}
	if got := statsOf([]time.Duration{2 * time.Hour}).Quantile(0.5); got > 2*time.Hour || got < time.Minute {
		t.Errorf("a request slower than every bound: p50 = %v, want between the last bound and the maximum", got)
	}
}
