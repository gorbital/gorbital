// Package jobs runs background jobs on PostgreSQL with River (ADR-0033).
//
// Jobs are declared as definitions: code written and deployed by developers,
// with configuration (enabled, schedule, timeout, retries, queue) that
// operators change at runtime through a [Manager], like serverless functions:
//
//	defs := jobs.NewDefinitions()
//	jobs.Define(defs, jobs.Definition[cleanupsessions.Args]{
//		Name:     "cleanup_sessions",
//		Worker:   cleanupsessions.NewWorker(store),
//		NewArgs:  func() cleanupsessions.Args { return cleanupsessions.Args{} },
//		Enabled:  true,
//		Schedule: "0 3 * * *",
//	})
//	workers := river.NewWorkers()
//	_ = jobs.AddMailWorker(workers, resendSender)
//	client, err := jobs.New(pool, workers,
//		jobs.WithQueues(jobs.DefaultQueues()), jobs.WithDefinitions(defs))
//
// The client is an app.Runner. Jobs enqueued from a request carry its request
// ID, trace and actor (but never its permissions); workers see them in their
// context (ADR-0030). River's tables are created by [Migrate].
//
// Stability: pre-1.0 (ADR-0015).
package jobs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Defaults applied by [New].
const (
	DefaultStopTimeout        = 20 * time.Second
	DefaultCompletedRetention = time.Hour
	DefaultCancelledRetention = 24 * time.Hour
	DefaultDiscardedRetention = 7 * 24 * time.Hour
)

const instrumentationName = "gorbital.dev/modules/jobs"

// Client enqueues and works jobs. It is safe for concurrent use.
type Client struct {
	river   *river.Client[pgx.Tx]
	working bool
	queues  map[string]river.QueueConfig
	defs    *Definitions
	sched   scheduler
}

type options struct {
	queues             map[string]river.QueueConfig
	defs               *Definitions
	logger             *slog.Logger
	tracerProvider     trace.TracerProvider
	propagator         propagation.TextMapPropagator
	stopTimeout        time.Duration
	completedRetention time.Duration
	cancelledRetention time.Duration
	discardedRetention time.Duration
	jobTimeout         time.Duration
	maxAttempts        int
}

// An Option configures [New].
type Option interface{ apply(*options) }

type optionFunc func(*options)

func (f optionFunc) apply(o *options) { f(o) }

// DefaultQueues returns the default queue with 10 workers per instance.
func DefaultQueues() map[string]river.QueueConfig {
	return map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 10}}
}

// WithQueues makes the client work jobs from queues. Without it the client
// only enqueues, as an API process does when a separate worker process runs
// jobs.
func WithQueues(queues map[string]river.QueueConfig) Option {
	return optionFunc(func(o *options) { o.queues = maps.Clone(queues) })
}

// WithDefinitions registers job definitions: their workers, their live
// configuration and, on working clients, their schedules. Use the same
// definitions for API and worker processes.
func WithDefinitions(defs *Definitions) Option {
	return optionFunc(func(o *options) { o.defs = defs })
}

// WithLogger sets the logger for job failures and River's own messages.
// Default: discard.
func WithLogger(logger *slog.Logger) Option {
	return optionFunc(func(o *options) { o.logger = logger })
}

// WithTracerProvider sets the provider for job spans. Default: the global
// OpenTelemetry provider. Trace context is propagated with the global
// propagator.
func WithTracerProvider(tp trace.TracerProvider) Option {
	return optionFunc(func(o *options) { o.tracerProvider = tp })
}

// WithPropagator sets how trace context is stored in job metadata. Default:
// the global OpenTelemetry propagator, which telemetry.Setup configures.
func WithPropagator(p propagation.TextMapPropagator) Option {
	return optionFunc(func(o *options) { o.propagator = p })
}

// WithStopTimeout sets how long running jobs may continue after shutdown
// starts before their contexts are cancelled. Keep it below the app's
// shutdown timeout. Default: [DefaultStopTimeout].
func WithStopTimeout(d time.Duration) Option {
	return optionFunc(func(o *options) { o.stopTimeout = d })
}

// WithRetention sets how long finished jobs are kept. -1 keeps them forever.
// Job arguments may contain personal data such as email bodies, so keep
// completed jobs briefly. Defaults: [DefaultCompletedRetention],
// [DefaultCancelledRetention], [DefaultDiscardedRetention].
func WithRetention(completed, cancelled, discarded time.Duration) Option {
	return optionFunc(func(o *options) {
		o.completedRetention, o.cancelledRetention, o.discardedRetention = completed, cancelled, discarded
	})
}

// WithJobTimeout sets the time a job without a definition may run. Default:
// River's, one minute.
func WithJobTimeout(d time.Duration) Option {
	return optionFunc(func(o *options) { o.jobTimeout = d })
}

// WithMaxAttempts sets the attempts for jobs without a definition before
// they are discarded. Default: River's, 25.
func WithMaxAttempts(n int) Option {
	return optionFunc(func(o *options) { o.maxAttempts = n })
}

// New builds a client on pool. workers may be nil for an insert-only client
// or when every job comes from definitions.
func New(pool *pgxpool.Pool, workers *river.Workers, opts ...Option) (*Client, error) {
	o := options{
		logger:             slog.New(slog.DiscardHandler),
		tracerProvider:     otel.GetTracerProvider(),
		propagator:         otel.GetTextMapPropagator(),
		stopTimeout:        DefaultStopTimeout,
		completedRetention: DefaultCompletedRetention,
		cancelledRetention: DefaultCancelledRetention,
		discardedRetention: DefaultDiscardedRetention,
	}
	for _, opt := range opts {
		opt.apply(&o)
	}
	working := len(o.queues) > 0
	if workers == nil && working && o.defs != nil {
		workers = river.NewWorkers()
	}

	var errs []error
	if pool == nil {
		errs = append(errs, errors.New("pool is required"))
	}
	if working && workers == nil {
		errs = append(errs, errors.New("workers or definitions are required to work queues"))
	}
	if o.stopTimeout <= 0 {
		errs = append(errs, errors.New("stop timeout must be positive"))
	}
	if o.logger == nil {
		errs = append(errs, errors.New("logger must not be nil"))
	}
	if o.tracerProvider == nil {
		errs = append(errs, errors.New("tracer provider must not be nil"))
	}
	if o.propagator == nil {
		errs = append(errs, errors.New("propagator must not be nil"))
	}
	if o.jobTimeout < 0 || o.maxAttempts < 0 {
		errs = append(errs, errors.New("job timeout and max attempts must not be negative"))
	}
	if o.defs != nil && working {
		for _, d := range o.defs.list() {
			if _, ok := o.queues[d.defaults.Queue]; !ok {
				errs = append(errs, fmt.Errorf("definition %s uses queue %q, which this client doesn't work", d.name, d.defaults.Queue))
			}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("jobs: invalid client: %w", err)
	}

	middleware := []rivertype.Middleware{newCorrelation(o.tracerProvider, o.propagator)}
	if o.defs != nil {
		if workers != nil {
			if err := o.defs.freeze(workers); err != nil {
				return nil, fmt.Errorf("jobs: %w", err)
			}
		}
		middleware = append(middleware, &applyDefinitions{defs: o.defs})
	}

	cfg := &river.Config{
		Queues:                      o.queues,
		Workers:                     workers,
		Logger:                      o.logger,
		Middleware:                  middleware,
		ErrorHandler:                &errorHandler{logger: o.logger},
		SoftStopTimeout:             o.stopTimeout,
		CompletedJobRetentionPeriod: o.completedRetention,
		CancelledJobRetentionPeriod: o.cancelledRetention,
		DiscardedJobRetentionPeriod: o.discardedRetention,
		JobTimeout:                  o.jobTimeout,
		MaxAttempts:                 o.maxAttempts,
	}
	rc, err := river.NewClient(riverpgxv5.New(pool), cfg)
	if err != nil {
		return nil, fmt.Errorf("jobs: create client: %w", err)
	}
	return &Client{river: rc, working: working, queues: o.queues, defs: o.defs}, nil
}

// Run works jobs until ctx is done, then stops: no new jobs are fetched,
// and running jobs get the stop timeout before their contexts are cancelled.
// It returns nil after a clean stop. An insert-only client just waits for
// ctx.
func (c *Client) Run(ctx context.Context) error {
	if !c.working {
		<-ctx.Done()
		return nil
	}
	if err := c.river.Start(ctx); err != nil {
		return fmt.Errorf("jobs: start: %w", err)
	}
	if err := c.applySchedules(); err != nil {
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return errors.Join(err, c.river.StopAndCancel(stopCtx))
	}
	<-c.river.Stopped()
	if ctx.Err() == nil {
		return errors.New("jobs: client stopped before shutdown")
	}
	return nil
}

// applySchedules brings River's periodic jobs in line with the definitions'
// effective schedules. It does nothing on insert-only clients.
func (c *Client) applySchedules() error {
	if !c.working || c.defs == nil {
		return nil
	}
	return c.sched.reconcile(c.river, c.defs)
}

// Insert enqueues a job. The job carries ctx's request ID, trace and actor.
func (c *Client) Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error) {
	res, err := c.river.Insert(ctx, args, opts)
	if err != nil {
		return nil, fmt.Errorf("jobs: insert %s: %w", args.Kind(), err)
	}
	return res, nil
}

// InsertTx enqueues a job in tx: it becomes visible to workers only if tx
// commits.
func (c *Client) InsertTx(ctx context.Context, tx pgx.Tx, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error) {
	res, err := c.river.InsertTx(ctx, tx, args, opts)
	if err != nil {
		return nil, fmt.Errorf("jobs: insert %s: %w", args.Kind(), err)
	}
	return res, nil
}

// River returns the underlying River client for advanced use.
func (c *Client) River() *river.Client[pgx.Tx] { return c.river }
