// Package idempotency makes retried POST and PATCH requests safe (ADR-0060).
// A client sends an Idempotency-Key header; the first request with the key
// runs, and its response is stored in PostgreSQL for the retention period so
// a retry gets the same response instead of running the request again:
//
//	store, err := idempotency.NewStore(pool, idempotency.WithRetention(retention.Get))
//	handler = idempotency.Middleware(store, idempotency.WithSkip(isSignIn))(handler)
//
// Keys belong to the caller that sent them: the signed-in user or service
// account in the context (gorbital.dev/actor). Requests without one ignore
// the header, so two callers never share a key or see each other's
// responses. A key sent again with another method, path, query or body is
// refused with 422 idempotency_key_reused, and a key whose first request is
// still running with 409 idempotency_in_progress.
//
// Only final outcomes are stored. Server errors (5xx), panics, and 401, 403,
// 408 and 429 responses release the key so the client can retry, and so do
// responses that set cookies or say Cache-Control: no-store, bodies larger
// than the cap, and requests whose handler calls [DontStore]. A request that dies without releasing its key
// (a crashed instance) holds it for the lock TTL.
//
// Stability: stable (ADR-0015, ADR-0054).
package idempotency

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	// Header is the request header carrying the client's key.
	Header = "Idempotency-Key"
	// ReplayedHeader is set to "true" on responses replayed from the store.
	ReplayedHeader = "Idempotent-Replayed"
	// MaxKeyLength is the longest key accepted, in bytes.
	MaxKeyLength = 255

	// DefaultRetention is how long responses are kept without
	// [WithRetention].
	DefaultRetention = 24 * time.Hour
	// DefaultLockTTL is how long a request holds its key without
	// [WithLockTTL]. It is well above the HTTP server's write timeout, so a
	// slow request isn't run twice, and short enough that a key held by a
	// crashed instance frees up within minutes.
	DefaultLockTTL = 5 * time.Minute

	// claimAttempts bounds how often Claim retries a key that changed
	// between its two statements.
	claimAttempts = 3
	scope         = "gorbital.dev/modules/idempotency"
)

// Store keeps idempotency keys and their responses in the idempotency_keys
// table. It is safe for concurrent use.
type Store struct {
	pool      *pgxpool.Pool
	logger    *slog.Logger
	retention func(context.Context) time.Duration
	lockTTL   time.Duration
	now       func() time.Time // nil: the database clock
	requests  metric.Int64Counter
}

// An Option configures a [Store].
type Option func(*Store)

// WithRetention reads how long responses are kept, on every use, so a
// runtime setting applies at once, to existing keys too. Default:
// [DefaultRetention].
func WithRetention(retention func(context.Context) time.Duration) Option {
	return func(s *Store) { s.retention = retention }
}

// WithLockTTL sets how long a request holds its key before another request
// with the same key may take it over. Default: [DefaultLockTTL].
func WithLockTTL(ttl time.Duration) Option { return func(s *Store) { s.lockTTL = ttl } }

// WithLogger sets the logger for keys that couldn't be stored or released.
// Default: discard.
func WithLogger(logger *slog.Logger) Option { return func(s *Store) { s.logger = logger } }

// WithClock uses now instead of the database's time, for tests.
func WithClock(now func() time.Time) Option { return func(s *Store) { s.now = now } }

// NewStore returns a store on pool, which must have the module's migrations
// applied. The middleware counts the OpenTelemetry metric
// idempotency.requests by outcome.
func NewStore(pool *pgxpool.Pool, opts ...Option) (*Store, error) {
	if pool == nil {
		return nil, errors.New("idempotency: a pool is required")
	}
	s := &Store{
		pool:      pool,
		logger:    slog.New(slog.DiscardHandler),
		retention: func(context.Context) time.Duration { return DefaultRetention },
		lockTTL:   DefaultLockTTL,
	}
	for _, o := range opts {
		o(s)
	}
	if s.retention == nil || s.lockTTL <= 0 {
		return nil, errors.New("idempotency: retention and lock TTL are required")
	}
	var err error
	if s.requests, err = otel.Meter(scope).Int64Counter("idempotency.requests",
		metric.WithDescription("Requests with an idempotency key by outcome")); err != nil {
		return nil, err
	}
	return s, nil
}

// Response is a stored response.
type Response struct {
	Status int
	// Header holds only the headers worth replaying: Content-Type,
	// Location and ETag.
	Header http.Header
	Body   []byte
}

// Lock is a claimed key. Exactly one of [Lock.Complete] and [Lock.Release]
// should be called.
type Lock struct {
	store *Store
	id    []byte
	token []byte
}

// Fingerprint identifies a request for comparing retries: its method, target
// (path and query, as in http.Request.RequestURI) and body.
func Fingerprint(method, target string, body []byte) []byte {
	bodySum := sha256.Sum256(body)
	h := sha256.New()
	h.Write([]byte(method + "\x00" + target + "\x00"))
	h.Write(bodySum[:])
	return h.Sum(nil)
}

// Claim takes key for the caller identified by scope. It returns a [Lock]
// when the request should run, or the stored [Response] when a request with
// the same fingerprint already completed. It returns [ErrInProgress] while
// another request holds the key, and [ErrKeyReused] when the key was used
// with another fingerprint. Keys held longer than the lock TTL, and keys
// older than the retention, can be claimed again.
func (s *Store) Claim(ctx context.Context, scope, key string, fingerprint []byte) (*Lock, *Response, error) {
	if scope == "" || key == "" || len(fingerprint) == 0 {
		return nil, nil, errors.New("idempotency: scope, key and fingerprint are required")
	}
	lock := &Lock{store: s, id: keyID(scope, key), token: make([]byte, 16)}
	_, _ = rand.Read(lock.token) // never fails (crypto/rand)
	retention := s.retention(ctx)
	for range claimAttempts {
		claimed, err := claimKey(ctx, s.pool, claimArgs{
			id: lock.id, fingerprint: fingerprint, token: lock.token,
			lockTTL: s.lockTTL, retention: retention, now: s.clock(),
		})
		if err != nil {
			return nil, nil, fmt.Errorf("idempotency: claim key: %v", err) //nolint:errorlint // driver errors aren't API (ADR-0018)
		}
		if claimed {
			return lock, nil, nil
		}
		row, err := selectKey(ctx, s.pool, lock.id)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // released since: claim it again
		}
		if err != nil {
			return nil, nil, fmt.Errorf("idempotency: read key: %v", err) //nolint:errorlint // driver errors aren't API (ADR-0018)
		}
		switch {
		case string(row.fingerprint) != string(fingerprint):
			return nil, nil, ErrKeyReused
		case row.status == nil:
			return nil, nil, ErrInProgress
		}
		resp := &Response{Status: int(*row.status), Header: http.Header{}, Body: row.body}
		if len(row.header) > 0 {
			if err := json.Unmarshal(row.header, &resp.Header); err != nil {
				return nil, nil, fmt.Errorf("idempotency: stored header: %w", err)
			}
		}
		return nil, resp, nil
	}
	// The key keeps changing hands: another request holds it now.
	return nil, nil, ErrInProgress
}

// Complete stores resp for the lock's key and gives the key up. It returns
// [ErrLockLost] when the lock no longer holds the key.
func (l *Lock) Complete(ctx context.Context, resp Response) error {
	header, err := json.Marshal(resp.Header)
	if err != nil {
		return fmt.Errorf("idempotency: encode header: %w", err)
	}
	n, err := completeKey(ctx, l.store.pool, l.id, l.token, resp.Status, header, resp.Body)
	if err != nil {
		return fmt.Errorf("idempotency: store response: %v", err) //nolint:errorlint // driver errors aren't API (ADR-0018)
	}
	if n == 0 {
		return ErrLockLost
	}
	return nil
}

// Release deletes the lock's key without storing a response, so the next
// request with it runs. Releasing a lost lock does nothing.
func (l *Lock) Release(ctx context.Context) error {
	if err := releaseKey(ctx, l.store.pool, l.id, l.token); err != nil {
		return fmt.Errorf("idempotency: release key: %v", err) //nolint:errorlint // driver errors aren't API (ADR-0018)
	}
	return nil
}

// clock returns the time to use instead of the database clock, or nil.
func (s *Store) clock() *time.Time {
	if s.now == nil {
		return nil
	}
	now := s.now()
	return &now
}

func (s *Store) count(ctx context.Context, outcome string) {
	s.requests.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
}

// keyID keeps callers' IDs and keys out of the table, and separates callers
// that send the same key.
func keyID(scope, key string) []byte {
	sum := sha256.Sum256([]byte(scope + "\x00" + key))
	return sum[:]
}
