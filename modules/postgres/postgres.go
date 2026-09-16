// Package postgres connects gorbital apps to PostgreSQL: a pgx connection
// pool with OpenTelemetry tracing, transactions, error classification for
// repositories, a readiness check and goose migrations (ADR-0005, ADR-0032).
//
// Repositories hold a [DBTX], so the same store runs on the pool or inside a
// transaction started with [InTx]. Transactions are passed explicitly, never
// stored in a context (ADR-0030).
//
// This package is the pgx adapter, so pgx types appear in its API and its
// errors wrap pgx errors. Repositories translate them into domain errors with
// [IsNoRows], [UniqueViolation] and the other helpers before returning.
//
// Stability: stable (ADR-0015, ADR-0054).
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"

	"gorbital.dev/config"
)

// DefaultConnectTimeout bounds each connection attempt and the initial ping.
const DefaultConnectTimeout = 5 * time.Second

// DBTX runs queries. *pgxpool.Pool, *pgxpool.Conn, *pgx.Conn and pgx.Tx
// implement it, so repositories work with or without a transaction.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

var (
	_ DBTX = (*pgxpool.Pool)(nil)
	_ DBTX = (pgx.Tx)(nil)
)

type options struct {
	maxConns        int32
	minConns        int32
	maxConnLifetime time.Duration
	maxConnIdleTime time.Duration
	connectTimeout  time.Duration
	applicationName string
	tracerProvider  trace.TracerProvider
}

func (o options) validate() error {
	var errs []error
	if o.maxConns < 0 {
		errs = append(errs, fmt.Errorf("max connections %d must not be negative", o.maxConns))
	}
	if o.minConns < 0 {
		errs = append(errs, fmt.Errorf("min connections %d must not be negative", o.minConns))
	}
	if o.maxConns > 0 && o.minConns > o.maxConns {
		errs = append(errs, fmt.Errorf("min connections %d exceed max connections %d", o.minConns, o.maxConns))
	}
	if o.maxConnLifetime < 0 || o.maxConnIdleTime < 0 {
		errs = append(errs, errors.New("connection lifetime and idle time must not be negative"))
	}
	if o.connectTimeout <= 0 {
		errs = append(errs, errors.New("connect timeout must be positive"))
	}
	if o.tracerProvider == nil {
		errs = append(errs, errors.New("tracer provider must not be nil"))
	}
	return errors.Join(errs...)
}

// An Option configures [Open].
type Option interface{ apply(*options) }

type optionFunc func(*options)

func (f optionFunc) apply(o *options) { f(o) }

// WithMaxConns sets the maximum pool size. Default: pgx's default, the
// greater of 4 and the number of CPUs.
func WithMaxConns(n int32) Option {
	return optionFunc(func(o *options) { o.maxConns = n })
}

// WithMinConns sets how many connections the pool keeps open when idle.
// Default: 0.
func WithMinConns(n int32) Option {
	return optionFunc(func(o *options) { o.minConns = n })
}

// WithMaxConnLifetime closes connections older than d, so load moves to new
// database hosts after failover. Default: one hour.
func WithMaxConnLifetime(d time.Duration) Option {
	return optionFunc(func(o *options) { o.maxConnLifetime = d })
}

// WithMaxConnIdleTime closes connections idle longer than d. Default: 30
// minutes.
func WithMaxConnIdleTime(d time.Duration) Option {
	return optionFunc(func(o *options) { o.maxConnIdleTime = d })
}

// WithConnectTimeout bounds each connection attempt and the ping in [Open].
// Default: [DefaultConnectTimeout].
func WithConnectTimeout(d time.Duration) Option {
	return optionFunc(func(o *options) { o.connectTimeout = d })
}

// WithApplicationName sets application_name, shown in pg_stat_activity.
func WithApplicationName(name string) Option {
	return optionFunc(func(o *options) { o.applicationName = name })
}

// WithTracerProvider sets the provider for query spans. Default: the global
// OpenTelemetry provider.
func WithTracerProvider(tp trace.TracerProvider) Option {
	return optionFunc(func(o *options) { o.tracerProvider = tp })
}

// Open creates a connection pool for url and pings the database, so a wrong
// URL or unreachable server fails at startup. Errors never include the URL,
// which may contain a password.
//
// Every query becomes an OpenTelemetry client span carrying the SQL text but
// never its arguments. Close the pool on shutdown:
//
//	cleanup.Add("postgres", func(context.Context) error { pool.Close(); return nil })
func Open(ctx context.Context, url config.Secret, opts ...Option) (*pgxpool.Pool, error) {
	o := options{connectTimeout: DefaultConnectTimeout, tracerProvider: otel.GetTracerProvider()}
	for _, opt := range opts {
		opt.apply(&o)
	}
	if err := o.validate(); err != nil {
		return nil, fmt.Errorf("postgres: invalid options: %w", err)
	}
	if url.IsZero() {
		return nil, errors.New("postgres: database URL is required")
	}

	cfg, err := pgxpool.ParseConfig(url.Reveal())
	if err != nil {
		// The parse error can quote the URL, so it isn't included.
		return nil, errors.New("postgres: database URL is not a valid PostgreSQL connection string")
	}
	if o.maxConns > 0 {
		cfg.MaxConns = o.maxConns
	}
	cfg.MinConns = o.minConns
	if o.maxConnLifetime > 0 {
		cfg.MaxConnLifetime = o.maxConnLifetime
	}
	if o.maxConnIdleTime > 0 {
		cfg.MaxConnIdleTime = o.maxConnIdleTime
	}
	cfg.ConnConfig.ConnectTimeout = o.connectTimeout
	if o.applicationName != "" {
		cfg.ConnConfig.RuntimeParams["application_name"] = o.applicationName
	}
	cfg.ConnConfig.Tracer = newTracer(o.tracerProvider)

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: create pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, o.connectTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: connect: %w", err)
	}
	return pool, nil
}
