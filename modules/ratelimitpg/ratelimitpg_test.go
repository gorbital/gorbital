package ratelimitpg_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"apistock.dev/modules/postgres/pgtest"
	"apistock.dev/modules/ratelimitpg"
	"apistock.dev/ratelimit"
)

func static(l ratelimit.Limit) func(context.Context) ratelimit.Limit {
	return func(context.Context) ratelimit.Limit { return l }
}

func newStore(t testing.TB, opts ...ratelimitpg.Option) (*ratelimitpg.Store, *pgxpool.Pool) {
	t.Helper()
	pool := pgtest.New(t, pgtest.WithMigrations(ratelimitpg.Migrations))
	s, err := ratelimitpg.NewStore(pool, opts...)
	if err != nil {
		t.Fatal(err)
	}
	return s, pool
}

func limiter(t testing.TB, s *ratelimitpg.Store, name string, limit func(context.Context) ratelimit.Limit) *ratelimitpg.Limiter {
	t.Helper()
	l, err := s.Limiter(name, limit)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// TestSharedAcrossInstances hammers one key from three limiters with the same
// name, as three app instances would: together they allow exactly the burst.
func TestSharedAcrossInstances(t *testing.T) {
	s, _ := newStore(t)
	instances := make([]*ratelimitpg.Limiter, 3)
	for i := range instances {
		instances[i] = limiter(t, s, "auth_login", static(ratelimit.Per(5, time.Hour)))
	}
	var allowed atomic.Int32
	var wg sync.WaitGroup
	for i := range 45 {
		wg.Go(func() {
			d, err := instances[i%3].Take(context.Background(), "ada@example.com")
			if err != nil {
				t.Error(err)
			}
			if d.Allowed {
				allowed.Add(1)
			} else if d.RetryAfter <= 0 {
				t.Errorf("refused without a retry time: %+v", d)
			}
		})
	}
	wg.Wait()
	if allowed.Load() != 5 {
		t.Errorf("allowed %d of 45 requests across 3 instances, want 5", allowed.Load())
	}

	// Another limiter name has its own budget for the same key.
	other := limiter(t, s, "auth_mfa", static(ratelimit.Per(5, time.Hour)))
	if d, _ := other.Take(context.Background(), "ada@example.com"); !d.Allowed {
		t.Error("a different limiter shares the key's budget")
	}
}

func TestLimitChangesAndCleanup(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	s, pool := newStore(t, ratelimitpg.WithClock(func() time.Time { return now }))
	ctx := context.Background()
	current := ratelimit.Per(2, time.Minute)
	l := limiter(t, s, "ip", func(context.Context) ratelimit.Limit { return current })

	take := func() ratelimit.Decision {
		t.Helper()
		d, err := l.Take(ctx, "203.0.113.7")
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	take()
	take()
	if d := take(); d.Allowed || d.RetryAfter != 30*time.Second {
		t.Fatalf("third request under 2 per minute = %+v, want refused for 30s", d)
	}
	// A runtime setting changed to 4 per 2 minutes: the next request uses it.
	current = ratelimit.Per(4, 2*time.Minute)
	if d := take(); !d.Allowed {
		t.Errorf("request after raising the limit = %+v, want allowed", d)
	}

	// Keys are stored hashed.
	var raw int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ratelimit_buckets WHERE position(convert_to('203.0.113.7', 'UTF8') in key) > 0`).Scan(&raw); err != nil || raw != 0 {
		t.Errorf("rows containing the raw key = %d, %v", raw, err)
	}

	if n, err := s.DeleteExpired(ctx, 100); err != nil || n != 0 {
		t.Errorf("DeleteExpired() while limited = %d, %v", n, err)
	}
	now = now.Add(2 * time.Minute)
	if n, err := s.DeleteExpired(ctx, 100); err != nil || n != 1 {
		t.Errorf("DeleteExpired() once full again = %d, %v", n, err)
	}
}

func TestFallbackWhenTheDatabaseFails(t *testing.T) {
	var logs bytes.Buffer
	s, pool := newStore(t, ratelimitpg.WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
	l := limiter(t, s, "auth_login", static(ratelimit.Per(3, time.Hour)))
	pool.Close()

	allowed := 0
	for range 6 {
		d, err := l.Take(context.Background(), "ada@example.com")
		if err != nil {
			t.Fatalf("Take() with the database down error = %v, want an in-memory decision", err)
		}
		if d.Allowed {
			allowed++
		}
	}
	if allowed != 3 {
		t.Errorf("allowed %d of 6 with the database down, want the burst of 3", allowed)
	}
	if n := strings.Count(logs.String(), "rate limits are per instance"); n != 1 {
		t.Errorf("fallback warnings = %d, want 1:\n%s", n, logs.String())
	}
}

// TestLocalPreCheck checks that requests far over the limit are refused
// without asking the database: after twice the burst, a closed pool causes no
// fallback.
func TestLocalPreCheck(t *testing.T) {
	var logs bytes.Buffer
	s, pool := newStore(t, ratelimitpg.WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
	l := limiter(t, s, "ip", static(ratelimit.Per(2, time.Hour)))
	for range 4 {
		if _, err := l.Take(context.Background(), "198.51.100.9"); err != nil {
			t.Fatal(err)
		}
	}
	pool.Close()
	d, err := l.Take(context.Background(), "198.51.100.9")
	if err != nil || d.Allowed || d.RetryAfter <= 0 {
		t.Errorf("request over twice the burst = %+v, %v; want refused locally", d, err)
	}
	if logs.Len() != 0 {
		t.Errorf("the refused request reached the database:\n%s", logs.String())
	}
}

func TestValidation(t *testing.T) {
	if _, err := ratelimitpg.NewStore(nil); err == nil {
		t.Error("NewStore(nil) error = nil")
	}
	s, _ := newStore(t)
	if _, err := s.Limiter("", static(ratelimit.Per(1, time.Minute))); err == nil {
		t.Error("Limiter(no name) error = nil")
	}
	l := limiter(t, s, "x", static(ratelimit.Limit{}))
	if _, err := l.Take(context.Background(), "k"); err == nil {
		t.Error("Take() with an invalid limit error = nil")
	}
	valid := limiter(t, s, "y", static(ratelimit.Per(1, time.Minute)))
	if _, err := valid.Take(context.Background(), ""); !errors.Is(err, ratelimit.ErrEmptyKey) {
		t.Errorf("Take(empty key) error = %v", err)
	}
}

func BenchmarkTake(b *testing.B) {
	s, _ := newStore(b)
	l, _ := s.Limiter("bench", static(ratelimit.Limit{PerSecond: 1e6, Burst: 1e6}))
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		if _, err := l.Take(ctx, "key"); err != nil {
			b.Fatal(err)
		}
	}
}
