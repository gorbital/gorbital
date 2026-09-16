// Package observability shows how an application is doing across all its
// instances and keeps a record of incidents (ADR-0064).
//
// A [Collector] on every instance counts HTTP requests per minute, method
// and route: requests, client and server errors, and a latency histogram.
// Run as a background runner, it writes its minutes to PostgreSQL every 15
// seconds through [Store.WriteMinutes], so [Store.Summary] can report
// request rates, error rates and latency percentiles over any recent window
// for the whole deployment, per instance and per route:
//
//	collector, err := observability.NewCollector(observability.WithInstance(tracker.InstanceID()), observability.WithSink(store))
//	handler := httpx.Chain(observability.RecordRoute(mux), httpx.Recover(logger), collector.Middleware(), …)
//
// Series are keyed by the route pattern registered in code, never the
// requested path, so clients can't grow them; each instance holds at most a
// bounded number of series for a bounded number of minutes.
//
// Incidents are opened by operators ([Store.OpenIncident]) or by
// [Store.DetectIncident] when the error rate crosses a threshold, and move
// through investigating, identified, monitoring and resolved with timeline
// updates. At most one automatic incident is open at a time, whichever
// instance runs detection.
//
// Stability: stable (ADR-0015, ADR-0054).
package observability

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"gorbital.dev/modules/postgres"
)

const (
	// DefaultQueryTimeout bounds summary queries without
	// [WithQueryTimeout].
	DefaultQueryTimeout = 5 * time.Second
	// MaxSummaryRange is the longest time range [Store.Summary] reports.
	MaxSummaryRange = 7 * 24 * time.Hour

	scope = "gorbital.dev/modules/observability"
	// writeBatch is how many minutes one insert statement writes.
	writeBatch = 1000
)

// Errors returned by [Store] methods. Check them with [errors.Is].
var (
	// ErrInvalidRange reports a summary range that is empty, reversed or
	// longer than [MaxSummaryRange].
	ErrInvalidRange = errors.New("observability: invalid time range")
	// ErrQueryTimeout reports a summary that took longer than the query
	// timeout.
	ErrQueryTimeout = errors.New("observability: query timed out")
	// ErrIncidentNotFound reports an incident ID that doesn't exist.
	ErrIncidentNotFound = errors.New("observability: incident not found")
	// ErrInvalidIncident reports an incident or update with a missing or
	// out-of-bounds field; the wrapping error says which.
	ErrInvalidIncident = errors.New("observability: invalid incident")
	// ErrIncidentResolved reports a change to a resolved incident.
	ErrIncidentResolved = errors.New("observability: incident is resolved")
	// ErrTooManyUpdates reports an incident that already has
	// [MaxIncidentUpdates] updates.
	ErrTooManyUpdates = errors.New("observability: incident has too many updates")
	// ErrInvalidCursor reports a cursor [Store.Incidents] didn't return.
	ErrInvalidCursor = errors.New("observability: invalid cursor")
)

// Store keeps request minutes and incidents in PostgreSQL. It is safe for
// concurrent use.
type Store struct {
	pool         *pgxpool.Pool
	now          func() time.Time
	queryTimeout time.Duration
	detections   metric.Int64Counter
}

// A StoreOption configures a [Store].
type StoreOption func(*Store)

// WithStoreClock uses now instead of time.Now, for tests.
func WithStoreClock(now func() time.Time) StoreOption { return func(s *Store) { s.now = now } }

// WithQueryTimeout bounds [Store.Summary]. Default: [DefaultQueryTimeout].
func WithQueryTimeout(d time.Duration) StoreOption {
	return func(s *Store) { s.queryTimeout = d }
}

// NewStore returns a store on pool, which must have the module's migrations
// applied. [Store.DetectIncident] counts the OpenTelemetry metric
// incidents.detections by action.
func NewStore(pool *pgxpool.Pool, opts ...StoreOption) (*Store, error) {
	if pool == nil {
		return nil, errors.New("observability: a pool is required")
	}
	s := &Store{pool: pool, now: time.Now, queryTimeout: DefaultQueryTimeout}
	for _, o := range opts {
		o(s)
	}
	if s.now == nil || s.queryTimeout <= 0 {
		return nil, errors.New("observability: clock and query timeout are required")
	}
	var err error
	if s.detections, err = otel.Meter(scope).Int64Counter("incidents.detections",
		metric.WithDescription("Automatic incident detection results that changed an incident, by action")); err != nil {
		return nil, err
	}
	return s, nil
}

func dbError(op string, err error) error {
	return fmt.Errorf("observability: %s: %v", op, err) //nolint:errorlint // driver errors aren't API (ADR-0018)
}

func (s *Store) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	return postgres.InTx(ctx, s.pool, fn)
}
