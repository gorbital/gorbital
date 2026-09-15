// Package ratelimitpg shares rate limits across instances in PostgreSQL
// (ADR-0052). A [Limiter] implements [ratelimit.Taker] with GCRA, the generic
// cell rate algorithm: each key keeps one row with its theoretical arrival
// time, and one statement decides and records a request, so every instance
// sees the same budget with the same rate-and-burst behaviour as the
// in-memory limiter. Keys are stored hashed.
//
// Two in-memory limiters protect it:
//
//   - a local pre-check with twice the burst refuses a client far over its
//     limit without touching the database, so a flood of requests can't
//     become a flood of writes;
//   - a fallback with the same limit decides when the database doesn't
//     answer within [DecisionTimeout], and a warning is logged at most once
//     a minute per limiter, so requests keep working with per-instance
//     limits during an outage.
//
// Stability: pre-1.0 (ADR-0015).
package ratelimitpg

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"gorbital.dev/ratelimit"
)

const (
	// DecisionTimeout bounds each database decision.
	DecisionTimeout = 250 * time.Millisecond
	// warnInterval is the least time between two fallback warnings of one
	// limiter.
	warnInterval = time.Minute
	scope        = "gorbital.dev/modules/ratelimitpg"
)

// Store holds the shared buckets of every limiter. It is safe for concurrent
// use.
type Store struct {
	pool      *pgxpool.Pool
	logger    *slog.Logger
	now       func() time.Time // nil: the database clock
	decisions metric.Int64Counter
	fallbacks metric.Int64Counter
}

// An Option configures a [Store].
type Option func(*Store)

// WithLogger sets the logger for fallback warnings. Default: discard.
func WithLogger(logger *slog.Logger) Option { return func(s *Store) { s.logger = logger } }

// WithClock makes decisions at now instead of the database's time, for
// tests.
func WithClock(now func() time.Time) Option { return func(s *Store) { s.now = now } }

// NewStore returns a store on pool, which must have the module's migrations
// applied. Decisions count the OpenTelemetry metrics ratelimit.decisions and
// ratelimit.fallbacks.
func NewStore(pool *pgxpool.Pool, opts ...Option) (*Store, error) {
	if pool == nil {
		return nil, errors.New("ratelimitpg: a pool is required")
	}
	s := &Store{pool: pool, logger: slog.New(slog.DiscardHandler)}
	for _, o := range opts {
		o(s)
	}
	meter := otel.Meter(scope)
	var err error
	if s.decisions, err = meter.Int64Counter("ratelimit.decisions", metric.WithDescription("Rate limit decisions by limiter, outcome and source")); err != nil {
		return nil, err
	}
	if s.fallbacks, err = meter.Int64Counter("ratelimit.fallbacks", metric.WithDescription("Decisions made in memory because the database didn't answer")); err != nil {
		return nil, err
	}
	return s, nil
}

// Limiter shares one limit across instances. Limiters with the same name on
// any instance share their keys' budgets. It is safe for concurrent use.
type Limiter struct {
	store *Store
	name  string
	limit func(context.Context) ratelimit.Limit

	mu       sync.Mutex
	current  ratelimit.Limit
	local    *ratelimit.Limiter // twice the burst: stops floods before the database
	fallback *ratelimit.Limiter // the same limit: decides when the database can't

	lastWarn atomic.Int64 // unix nanoseconds
}

// Limiter returns the limiter name, whose limit is read on every decision,
// so a limit backed by a runtime setting applies at once. The name is part of
// every key: use one name per kind of limit, such as auth_login.
func (s *Store) Limiter(name string, limit func(context.Context) ratelimit.Limit) (*Limiter, error) {
	if name == "" || limit == nil {
		return nil, errors.New("ratelimitpg: a limiter needs a name and a limit")
	}
	return &Limiter{store: s, name: name, limit: limit}, nil
}

// Take implements [ratelimit.Taker]. It fails only for an empty key or an
// invalid limit; database failures are decided in memory.
func (l *Limiter) Take(ctx context.Context, key string) (ratelimit.Decision, error) {
	if key == "" {
		return ratelimit.Decision{}, ratelimit.ErrEmptyKey
	}
	lim := l.limit(ctx)
	if !lim.Valid() {
		return ratelimit.Decision{}, fmt.Errorf("ratelimitpg: limiter %s has an invalid limit %+v", l.name, lim)
	}
	local, fallback := l.memory(lim)

	if ok, retry := local.Allow(key); !ok {
		l.count(ctx, false, "local")
		return ratelimit.Decision{RetryAfter: retry}, nil
	}
	d, err := l.store.take(ctx, l.name, key, lim)
	if err == nil {
		l.count(ctx, d.Allowed, "database")
		return d, nil
	}
	l.fellBack(ctx, err)
	ok, retry := fallback.Allow(key)
	l.count(ctx, ok, "fallback")
	return ratelimit.Decision{Allowed: ok, RetryAfter: retry}, nil
}

// memory returns the in-memory limiters for lim, rebuilding them when the
// limit changed.
func (l *Limiter) memory(lim ratelimit.Limit) (local, fallback *ratelimit.Limiter) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.local == nil || l.current != lim {
		var opts []ratelimit.Option
		if l.store.now != nil {
			opts = append(opts, ratelimit.WithClock(l.store.now))
		}
		l.current = lim
		l.local = ratelimit.New(lim.PerSecond, 2*lim.Burst, opts...)
		l.fallback = ratelimit.New(lim.PerSecond, lim.Burst, opts...)
	}
	return l.local, l.fallback
}

func (l *Limiter) fellBack(ctx context.Context, err error) {
	reason := "error"
	if errors.Is(err, context.DeadlineExceeded) {
		reason = "timeout"
	}
	l.store.fallbacks.Add(ctx, 1, metric.WithAttributes(attribute.String("limiter", l.name), attribute.String("reason", reason)))
	now := time.Now().UnixNano()
	last := l.lastWarn.Load()
	if now-last >= int64(warnInterval) && l.lastWarn.CompareAndSwap(last, now) {
		l.store.logger.WarnContext(ctx, "rate limits are per instance until the database answers again",
			"limiter", l.name, "reason", reason, "err", err)
	}
}

func (l *Limiter) count(ctx context.Context, allowed bool, source string) {
	l.store.decisions.Add(ctx, 1, metric.WithAttributes(
		attribute.String("limiter", l.name), attribute.Bool("allowed", allowed), attribute.String("source", source)))
}
