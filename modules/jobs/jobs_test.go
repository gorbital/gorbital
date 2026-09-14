package jobs_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"apistock.dev/actor"
	"apistock.dev/mail"
	"apistock.dev/modules/jobs"
	"apistock.dev/modules/postgres/pgtest"
	"apistock.dev/requestid"
)

func TestMigrate(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.New(t)

	pending, err := jobs.MigrationsPending(ctx, pool)
	if err != nil || len(pending) == 0 {
		t.Fatalf("MigrationsPending() on an empty database = %v, %v; want pending migrations", pending, err)
	}
	applied, err := jobs.Migrate(ctx, pool)
	if err != nil || len(applied) < 6 || applied[0] != 1 {
		t.Fatalf("Migrate() = %v, %v; want River's versions from 1", applied, err)
	}
	if again, err := jobs.Migrate(ctx, pool); err != nil || len(again) != 0 {
		t.Errorf("second Migrate() = %v, %v; want nothing applied", again, err)
	}
	if pending, err := jobs.MigrationsPending(ctx, pool); err != nil || pending != nil {
		t.Errorf("MigrationsPending() after Migrate = %v, %v; want none", pending, err)
	}
}

func TestNewValidatesOptions(t *testing.T) {
	defs := jobs.NewDefinitions()
	jobs.Define(defs, jobs.Definition[rebuildArgs]{
		Name:    "rebuild_index",
		Worker:  &observingWorker[rebuildArgs]{},
		NewArgs: func() rebuildArgs { return rebuildArgs{} },
		Queue:   "reports",
	})
	_, err := jobs.New(nil, nil,
		jobs.WithQueues(jobs.DefaultQueues()),
		jobs.WithDefinitions(defs),
		jobs.WithStopTimeout(0),
		jobs.WithPropagator(nil),
	)
	if err == nil {
		t.Fatal("New() error = nil")
	}
	for _, want := range []string{"pool is required", "stop timeout", "propagator", `queue "reports"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("New() error = %q, want it to mention %q", err, want)
		}
	}
	if _, err := jobs.New(nil, nil, jobs.WithQueues(jobs.DefaultQueues())); err == nil || !strings.Contains(err.Error(), "workers or definitions") {
		t.Errorf("New() with queues and no workers error = %v", err)
	}
}

func TestInvalidDefinitionsPanic(t *testing.T) {
	worker := &observingWorker[rebuildArgs]{}
	newArgs := func() rebuildArgs { return rebuildArgs{} }
	valid := func() jobs.Definition[rebuildArgs] {
		return jobs.Definition[rebuildArgs]{Name: "rebuild_index", Worker: worker, NewArgs: newArgs, Enabled: true}
	}
	tests := []struct {
		name   string
		mutate func(*jobs.Definition[rebuildArgs])
		want   string
	}{
		{"bad name", func(d *jobs.Definition[rebuildArgs]) { d.Name = "Rebuild-Index" }, "snake_case"},
		{"kind mismatch", func(d *jobs.Definition[rebuildArgs]) { d.Name = "other_job" }, "must equal the definition name"},
		{"no worker", func(d *jobs.Definition[rebuildArgs]) { d.Worker = nil }, "Worker and NewArgs"},
		{"bad cron", func(d *jobs.Definition[rebuildArgs]) { d.Schedule = "every night" }, "cron expression"},
		{"too frequent", func(d *jobs.Definition[rebuildArgs]) { d.Schedule = "@every 30s" }, "more often than every 1m"},
		{"timeout too long", func(d *jobs.Definition[rebuildArgs]) { d.Timeout = 48 * time.Hour }, "timeout must be between"},
		{"too many attempts", func(d *jobs.Definition[rebuildArgs]) { d.MaxAttempts = 500 }, "max attempts"},
		{"bad priority", func(d *jobs.Definition[rebuildArgs]) { d.Priority = 9 }, "priority"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if msg, _ := recover().(string); !strings.Contains(msg, tt.want) {
					t.Errorf("panic = %q, want it to contain %q", msg, tt.want)
				}
			}()
			d := valid()
			tt.mutate(&d)
			jobs.Define(jobs.NewDefinitions(), d)
		})
	}

	t.Run("duplicate", func(t *testing.T) {
		defer func() {
			if msg, _ := recover().(string); !strings.Contains(msg, "defined twice") {
				t.Errorf("panic = %q, want defined twice", msg)
			}
		}()
		defs := jobs.NewDefinitions()
		jobs.Define(defs, valid())
		jobs.Define(defs, valid())
	})
}

func TestJobsCarryTheEnqueuingContext(t *testing.T) {
	pool := newPool(t)
	seen := make(chan observed, 10)
	spans := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(spans))
	client := newWorkingClient(t, pool, defineJobs(seen),
		jobs.WithTracerProvider(tp), jobs.WithPropagator(propagation.TraceContext{}))
	run(t, client)

	ctx := actor.With(context.Background(), actor.Actor{
		Kind: actor.KindUser, ID: "usr_1", Label: "Ada", OrgID: "org_9", Permissions: []string{"ops.jobs.write"},
	})
	ctx = requestid.With(ctx, "req_ctx_1")
	ctx, span := tp.Tracer("test").Start(ctx, "POST /v1/indexes")
	res, err := client.Insert(ctx, rebuildArgs{}, nil)
	span.End()
	if err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	got := receive(t, seen, "the job to run")
	if got.jobID != res.Job.ID || got.requestID != "req_ctx_1" {
		t.Errorf("worker saw job %d with request ID %q, want job %d with req_ctx_1", got.jobID, got.requestID, res.Job.ID)
	}
	if !got.hasActor || got.actor.Kind != actor.KindSystem || got.actor.ID != "jobs" || got.actor.OrgID != "org_9" || len(got.actor.Permissions) != 0 {
		t.Errorf("worker actor = %+v, want system jobs actor in org_9 without permissions", got.actor)
	}
	if !got.hasOnBehalf || got.onBehalf.Kind != actor.KindUser || got.onBehalf.ID != "usr_1" || got.onBehalf.Label != "Ada" || len(got.onBehalf.Permissions) != 0 {
		t.Errorf("OnBehalfOf() = %+v, %v; want usr_1 without permissions", got.onBehalf, got.hasOnBehalf)
	}
	if got.timeout <= 50*time.Second || got.timeout > time.Minute {
		t.Errorf("job context deadline in %v, want the definition's 1m default timeout", got.timeout)
	}

	row, err := client.River().JobGet(context.Background(), res.Job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(row.Metadata), "ops.jobs.write") {
		t.Errorf("job metadata %s stores the enqueuing actor's permissions", row.Metadata)
	}

	waitFor(t, "the job span", func() bool {
		for _, s := range spans.GetSpans() {
			if s.Name == "job rebuild_index" && s.SpanKind == trace.SpanKindConsumer &&
				s.SpanContext.TraceID() == span.SpanContext().TraceID() && s.Parent.SpanID() == span.SpanContext().SpanID() {
				return true
			}
		}
		return false
	})
}

type fakeSender struct {
	sent chan mail.Message
}

func (f fakeSender) Send(_ context.Context, m mail.Message) error {
	f.sent <- m
	return nil
}

func TestAsyncSenderDeliversThroughTheMailWorker(t *testing.T) {
	pool := newPool(t)
	sender := fakeSender{sent: make(chan mail.Message, 10)}
	workers := river.NewWorkers()
	if err := jobs.AddMailWorker(workers, sender); err != nil {
		t.Fatal(err)
	}
	if err := jobs.AddMailWorker(workers, sender); err == nil {
		t.Error("registering the mail worker twice succeeded")
	}
	if err := jobs.AddMailWorker(workers, nil); err == nil {
		t.Error("AddMailWorker(nil sender) succeeded")
	}
	client, err := jobs.New(pool, workers, jobs.WithQueues(jobs.DefaultQueues()))
	if err != nil {
		t.Fatal(err)
	}
	run(t, client)
	mailer := jobs.AsyncSender(client)

	msg := mail.Message{
		From:    mail.Address{Name: "Acme", Email: "no-reply@acme.test"},
		To:      []mail.Address{{Email: "ada@example.com"}},
		ReplyTo: []mail.Address{{Name: "Support", Email: "support@acme.test"}},
		Subject: "Your code",
		Text:    "123456",
		HTML:    "<p>123456</p>",
		Tags:    map[string]string{"category": "verification"},
	}
	if err := mailer.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	delivered := receive(t, sender.sent, "the queued email")
	if !strings.HasPrefix(delivered.IdempotencyKey, "job-") {
		t.Errorf("IdempotencyKey = %q, want job-<id>", delivered.IdempotencyKey)
	}
	delivered.IdempotencyKey = ""
	if delivered.Subject != msg.Subject || delivered.HTML != msg.HTML || delivered.ReplyTo[0] != msg.ReplyTo[0] ||
		delivered.To[0] != msg.To[0] || delivered.From != msg.From || delivered.Tags["category"] != "verification" {
		t.Errorf("delivered = %+v, want %+v", delivered, msg)
	}

	msg.IdempotencyKey = "welcome-usr_1"
	if err := mailer.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if got := receive(t, sender.sent, "the second email").IdempotencyKey; got != "welcome-usr_1" {
		t.Errorf("IdempotencyKey = %q, want the caller's key kept", got)
	}

	if err := mailer.Send(context.Background(), mail.Message{Subject: "no recipients"}); err == nil {
		t.Error("Send(invalid message) error = nil")
	}
	res, err := client.River().JobList(context.Background(), river.NewJobListParams().Kinds(jobs.MailKind))
	if err != nil || len(res.Jobs) != 2 {
		t.Errorf("mail jobs = %d, %v; want 2 (the invalid message is never queued)", len(res.Jobs), err)
	}
}

type flakyArgs struct{}

func (flakyArgs) Kind() string { return "flaky" }

type flakyWorker struct {
	river.WorkerDefaults[flakyArgs]
}

func (flakyWorker) Work(context.Context, *river.Job[flakyArgs]) error {
	return errors.New("provider unavailable")
}

func TestFailedAttemptsAreLoggedOnce(t *testing.T) {
	pool := newPool(t)
	logs := &syncBuffer{}
	workers := river.NewWorkers()
	river.AddWorker(workers, &flakyWorker{})
	client, err := jobs.New(pool, workers,
		jobs.WithQueues(jobs.DefaultQueues()),
		jobs.WithLogger(slog.New(slog.NewJSONHandler(logs, nil))),
	)
	if err != nil {
		t.Fatal(err)
	}
	run(t, client)

	res, err := client.Insert(context.Background(), flakyArgs{}, &river.InsertOpts{MaxAttempts: 3})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the failure log", func() bool { return strings.Contains(logs.String(), `"msg":"job failed"`) })
	line := ""
	for _, l := range strings.Split(logs.String(), "\n") {
		if strings.Contains(l, `"msg":"job failed"`) {
			line = l
			break
		}
	}
	for _, want := range []string{`"level":"WARN"`, `"job_kind":"flaky"`, `"attempt":1`, `"max_attempts":3`, `provider unavailable`} {
		if !strings.Contains(line, want) {
			t.Errorf("failure log %s lacks %s", line, want)
		}
	}
	if n := strings.Count(logs.String(), `"msg":"job failed"`); n != 1 {
		t.Errorf("logged the first failed attempt %d times, want once", n)
	}
	// River logs through the error handler before it records the attempt.
	waitFor(t, "the job to become retryable", func() bool {
		row, err := client.River().JobGet(context.Background(), res.Job.ID)
		return err == nil && row.State == rivertype.JobStateRetryable
	})
}

type slowArgs struct{}

func (slowArgs) Kind() string { return "slow" }

type slowWorker struct {
	river.WorkerDefaults[slowArgs]
	started chan struct{}
}

func (w *slowWorker) Work(ctx context.Context, _ *river.Job[slowArgs]) error {
	close(w.started)
	<-ctx.Done()
	return ctx.Err()
}

func TestRunStopsGracefullyWithinTheStopTimeout(t *testing.T) {
	pool := newPool(t)
	worker := &slowWorker{started: make(chan struct{})}
	workers := river.NewWorkers()
	river.AddWorker(workers, worker)
	client, err := jobs.New(pool, workers, jobs.WithQueues(jobs.DefaultQueues()), jobs.WithStopTimeout(300*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- client.Run(ctx) }()
	if _, err := client.Insert(context.Background(), slowArgs{}, nil); err != nil {
		t.Fatal(err)
	}
	receive(t, worker.started, "the slow job to start")

	start := time.Now()
	cancel()
	err = receive(t, done, "Run to return")
	if err != nil {
		t.Errorf("Run() error = %v", err)
	}
	if elapsed := time.Since(start); elapsed < 250*time.Millisecond || elapsed > 10*time.Second {
		t.Errorf("Run() returned %v after shutdown began, want about the 300ms stop timeout", elapsed)
	}
}

func TestInsertOnlyClientAndTransactionalInsert(t *testing.T) {
	ctx := context.Background()
	pool := newPool(t)
	client, err := jobs.New(pool, nil, jobs.WithDefinitions(defineJobs(nil)))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	run(t, client)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.InsertTx(ctx, tx, rebuildArgs{}, nil); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if n := countJobs(t, client, "rebuild_index"); n != 0 {
		t.Errorf("jobs after rollback = %d, want 0", n)
	}

	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, err := client.InsertTx(ctx, tx, rebuildArgs{}, nil)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if n := countJobs(t, client, "rebuild_index"); n != 1 {
		t.Errorf("jobs after commit = %d, want 1", n)
	}
}

func countJobs(t *testing.T, client *jobs.Client, kind string) int {
	t.Helper()
	res, err := client.River().JobList(context.Background(), river.NewJobListParams().Kinds(kind))
	if err != nil {
		t.Fatal(err)
	}
	return len(res.Jobs)
}
