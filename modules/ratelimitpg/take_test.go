package ratelimitpg

import (
	"context"
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"apistock.dev/modules/postgres/pgtest"
	"apistock.dev/ratelimit"
)

// model is GCRA in integer microseconds: the reference the SQL must match.
type model struct {
	tat      map[string]int64
	interval int64
	tol      int64
}

func (m *model) take(key string, now int64) (bool, int64) {
	tat, seen := m.tat[key]
	arrival := max(tat, now)
	if !seen || arrival+m.interval-now <= m.tol {
		m.tat[key] = arrival + m.interval
		return true, 0
	}
	return false, max(arrival+m.interval-now-m.tol, 1)
}

// TestTakeMatchesModel checks, for random limits and request times, that the
// database decides every request and its retry time exactly as GCRA does.
func TestTakeMatchesModel(t *testing.T) {
	pool := pgtest.New(t, pgtest.WithMigrations(Migrations))
	rng := rand.New(rand.NewPCG(52, 2026))
	start := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	for c := range 25 {
		lim := ratelimit.Limit{PerSecond: []float64{0.2, 1, 3, 7.5, 100}[rng.IntN(5)], Burst: 1 + rng.IntN(6)}
		interval, tolerance := gcra(lim)
		m := &model{tat: map[string]int64{}, interval: interval.Microseconds(), tol: tolerance.Microseconds()}
		now := start
		s := &Store{pool: pool, now: func() time.Time { return now }}
		key := fmt.Sprintf("case-%d", c)
		for i := range 60 {
			now = now.Add(time.Duration(rng.Int64N(int64(3*interval) + 1)).Truncate(time.Microsecond))
			d, err := s.take(context.Background(), "model", key, lim)
			if err != nil {
				t.Fatal(err)
			}
			allowed, retry := m.take(key, now.Sub(start).Microseconds())
			if d.Allowed != allowed || d.RetryAfter.Microseconds() != retry {
				t.Fatalf("case %d (%+v) request %d: database = %+v, model = allowed %v retry %dµs", c, lim, i, d, allowed, retry)
			}
		}
	}
}

func TestGCRAInterval(t *testing.T) {
	interval, tolerance := gcra(ratelimit.Per(10, 15*time.Minute))
	if interval != 90*time.Second || tolerance != 900*time.Second {
		t.Errorf("gcra(10 per 15m) = %s, %s", interval, tolerance)
	}
	if interval, _ := gcra(ratelimit.Limit{PerSecond: 1e9, Burst: 1}); interval != time.Microsecond {
		t.Errorf("gcra(very fast) interval = %s, want at least 1µs", interval)
	}
}
