package jobs_test

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"gorbital.dev/actor"
	"gorbital.dev/audit"
	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/postgres/pgtest"
	"gorbital.dev/requestid"
)

// newPool returns a database with the jobs definition tables and River's
// schema.
func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := pgtest.New(t, pgtest.WithMigrations(jobs.Migrations), pgtest.WithMaxConns(40))
	if _, err := jobs.Migrate(context.Background(), pool); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	return pool
}

// run runs r until the test ends and checks it stops cleanly.
func run(t *testing.T, r interface{ Run(context.Context) error }) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run() error = %v", err)
			}
		case <-time.After(30 * time.Second):
			t.Error("Run() did not return after its context was cancelled")
		}
	})
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func receive[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(20 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		var zero T
		return zero
	}
}

func operator() context.Context {
	ctx := actor.With(context.Background(), actor.Actor{Kind: actor.KindUser, ID: "usr_ops", Label: "Ops"})
	return requestid.With(ctx, "req_ops_1")
}

type cleanupArgs struct{}

func (cleanupArgs) Kind() string { return "cleanup_sessions" }

type rebuildArgs struct{}

func (rebuildArgs) Kind() string { return "rebuild_index" }

// observed is what a worker saw in its context.
type observed struct {
	jobID       int64
	requestID   string
	actor       actor.Actor
	hasActor    bool
	onBehalf    actor.Actor
	hasOnBehalf bool
	timeout     time.Duration
}

type observingWorker[T river.JobArgs] struct {
	river.WorkerDefaults[T]
	seen chan<- observed
}

func (w *observingWorker[T]) Work(ctx context.Context, job *river.Job[T]) error {
	o := observed{jobID: job.ID, requestID: requestid.From(ctx)}
	o.actor, o.hasActor = actor.From(ctx)
	o.onBehalf, o.hasOnBehalf = jobs.OnBehalfOf(ctx)
	if deadline, ok := ctx.Deadline(); ok {
		o.timeout = time.Until(deadline)
	}
	select {
	case w.seen <- o:
	default:
	}
	return nil
}

// defineJobs declares a scheduled and an on-demand job.
func defineJobs(seen chan<- observed) *jobs.Definitions {
	defs := jobs.NewDefinitions()
	jobs.Define(defs, jobs.Definition[cleanupArgs]{
		Name:        "cleanup_sessions",
		Description: "Deletes expired sessions.",
		Worker:      &observingWorker[cleanupArgs]{seen: seen},
		NewArgs:     func() cleanupArgs { return cleanupArgs{} },
		Enabled:     true,
		Schedule:    "0 3 * * *",
		Timeout:     5 * time.Minute,
		MaxAttempts: 5,
	})
	jobs.Define(defs, jobs.Definition[rebuildArgs]{
		Name:    "rebuild_index",
		Worker:  &observingWorker[rebuildArgs]{seen: seen},
		NewArgs: func() rebuildArgs { return rebuildArgs{} },
		Enabled: true,
	})
	return defs
}

func newWorkingClient(t *testing.T, pool *pgxpool.Pool, defs *jobs.Definitions, opts ...jobs.Option) *jobs.Client {
	t.Helper()
	opts = append([]jobs.Option{jobs.WithQueues(jobs.DefaultQueues()), jobs.WithDefinitions(defs)}, opts...)
	client, err := jobs.New(pool, nil, opts...)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return client
}

type recorder struct {
	mu     sync.Mutex
	events []audit.Event
}

func (r *recorder) Record(_ context.Context, e audit.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	return nil
}

func (r *recorder) actions() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	for i, e := range r.events {
		out[i] = e.Action
	}
	return out
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
