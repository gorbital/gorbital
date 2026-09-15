package jobs_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"gorbital.dev/modules/jobs"
)

type managerSetup struct {
	pool    *pgxpool.Pool
	client  *jobs.Client
	manager *jobs.Manager
	rec     *recorder
	seen    chan observed
}

// newManager runs a working client, so the default queue is active, and a
// manager over it.
func newManager(t *testing.T, pool *pgxpool.Pool) managerSetup {
	t.Helper()
	s := managerSetup{pool: pool, rec: &recorder{}, seen: make(chan observed, 50)}
	s.client = newWorkingClient(t, pool, defineJobs(s.seen))
	run(t, s.client)
	var err error
	s.manager, err = jobs.NewManager(context.Background(), pool, s.client, s.rec)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	waitFor(t, "the default queue to become active", func() bool {
		queues, err := s.manager.Queues(context.Background())
		return err == nil && len(queues) > 0
	})
	return s
}

func ptr[T any](v T) *T { return &v }

func TestNewManagerValidatesDependencies(t *testing.T) {
	_, err := jobs.NewManager(context.Background(), nil, nil, nil, jobs.WithResyncInterval(0))
	if err == nil {
		t.Fatal("NewManager() error = nil")
	}
	for _, want := range []string{"pool is required", "WithDefinitions", "audit recorder", "resync interval"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("NewManager() error = %q, want it to mention %q", err, want)
		}
	}
}

func TestDefinitionsShowCodeDefaults(t *testing.T) {
	s := newManager(t, newPool(t))
	views, err := s.manager.Definitions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 || views[0].Name != "cleanup_sessions" || views[1].Name != "rebuild_index" {
		t.Fatalf("Definitions() = %+v", views)
	}
	cleanup := views[0]
	want := jobs.Config{Enabled: true, Schedule: "0 3 * * *", Timeout: 5 * time.Minute, MaxAttempts: 5, Queue: "default", Priority: 1}
	if cleanup.Config != want || cleanup.Defaults != want || cleanup.Modified || cleanup.Version != 0 || cleanup.Description == "" {
		t.Errorf("cleanup view = %+v", cleanup)
	}
	if next := cleanup.NextRunAt.UTC(); next.Hour() != 3 || next.Minute() != 0 || !next.After(time.Now()) {
		t.Errorf("NextRunAt = %v, want the next 03:00 UTC", cleanup.NextRunAt)
	}
	if !views[1].NextRunAt.IsZero() {
		t.Errorf("on-demand job NextRunAt = %v, want zero", views[1].NextRunAt)
	}
	scheduled, err := s.manager.Scheduled(context.Background())
	if err != nil || len(scheduled) != 1 || scheduled[0].Name != "cleanup_sessions" {
		t.Errorf("Scheduled() = %+v, %v; want only cleanup_sessions", scheduled, err)
	}
	if _, err := s.manager.Definition(context.Background(), "nope"); !errors.Is(err, jobs.ErrUnknownDefinition) {
		t.Errorf("Definition(unknown) error = %v", err)
	}
}

func TestUpdateResetAndHistory(t *testing.T) {
	ctx := operator()
	s := newManager(t, newPool(t))

	if _, err := s.manager.Update(ctx, "cleanup_sessions", jobs.ConfigPatch{Schedule: ptr("@every 2h")}, jobs.Change{}); !errors.Is(err, jobs.ErrReasonRequired) {
		t.Errorf("reschedule without reason error = %v, want ErrReasonRequired", err)
	}
	v, err := s.manager.Update(ctx, "cleanup_sessions", jobs.ConfigPatch{Schedule: ptr("@every 2h"), MaxAttempts: ptr(8)}, jobs.Change{Reason: "backlog"})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if v.Version != 1 || !v.Modified || v.Config.Schedule != "@every 2h" || v.Config.MaxAttempts != 8 || v.UpdatedBy != "usr_ops" {
		t.Errorf("view after Update = %+v", v)
	}
	if until := time.Until(v.NextRunAt); until < 119*time.Minute || until > 121*time.Minute {
		t.Errorf("NextRunAt in %v, want about 2h", until)
	}

	if _, err := s.manager.Update(ctx, "cleanup_sessions", jobs.ConfigPatch{MaxAttempts: ptr(9)}, jobs.Change{Version: 0}); !errors.Is(err, jobs.ErrVersionConflict) {
		t.Errorf("stale Update() error = %v, want ErrVersionConflict", err)
	}
	// Setting a field to its current value changes nothing.
	if v, err := s.manager.Update(ctx, "cleanup_sessions", jobs.ConfigPatch{MaxAttempts: ptr(8)}, jobs.Change{Version: 1}); err != nil || v.Version != 1 {
		t.Errorf("no-op Update() = version %d, %v; want 1", v.Version, err)
	}

	if _, err := s.manager.Update(ctx, "cleanup_sessions", jobs.ConfigPatch{Enabled: ptr(false)}, jobs.Change{Version: 1}); !errors.Is(err, jobs.ErrReasonRequired) {
		t.Errorf("disable without reason error = %v, want ErrReasonRequired", err)
	}
	v, err = s.manager.Update(ctx, "cleanup_sessions", jobs.ConfigPatch{Enabled: ptr(false)}, jobs.Change{Version: 1, Reason: "incident 42"})
	if err != nil || v.Config.Enabled || !v.NextRunAt.IsZero() || v.Version != 2 {
		t.Fatalf("disable = %+v, %v", v, err)
	}
	if _, err := s.manager.RunNow(ctx, "cleanup_sessions"); !errors.Is(err, jobs.ErrDefinitionDisabled) {
		t.Errorf("RunNow(disabled) error = %v, want ErrDefinitionDisabled", err)
	}
	if scheduled, _ := s.manager.Scheduled(ctx); len(scheduled) != 0 {
		t.Errorf("Scheduled() includes a disabled job: %+v", scheduled)
	}

	v, err = s.manager.Reset(ctx, "cleanup_sessions", jobs.Change{Version: 2, Reason: "incident resolved"})
	if err != nil || v.Modified || v.Version != 3 || v.Config != v.Defaults {
		t.Fatalf("Reset() = %+v, %v; want code defaults at version 3", v, err)
	}

	history, err := s.manager.History(ctx, "cleanup_sessions", 0, 10)
	if err != nil || len(history) != 3 {
		t.Fatalf("History() = %d entries, %v; want 3", len(history), err)
	}
	if history[0].Action != "reset" || string(history[0].NewConfig) != "{}" || history[0].Reason != "incident resolved" {
		t.Errorf("newest history entry = %+v", history[0])
	}
	var first map[string]any
	if err := json.Unmarshal(history[2].NewConfig, &first); err != nil || first["schedule"] != "@every 2h" || first["max_attempts"] != float64(8) {
		t.Errorf("first change new config = %s", history[2].NewConfig)
	}
	if history[2].ActorID != "usr_ops" || history[2].RequestID != "req_ops_1" || string(history[2].OldConfig) != "{}" {
		t.Errorf("first history entry = %+v", history[2])
	}
	if page, err := s.manager.History(ctx, "cleanup_sessions", history[1].ID, 5); err != nil || len(page) != 1 || page[0].ID != history[2].ID {
		t.Errorf("History(before) = %+v, %v", page, err)
	}

	if got := s.rec.actions(); len(got) != 3 || got[0] != "jobs.definition.changed" {
		t.Errorf("audit actions = %v, want 3 jobs.definition.changed", got)
	}
}

func TestUpdateRejectsInvalidChanges(t *testing.T) {
	ctx := operator()
	s := newManager(t, newPool(t))

	invalid := []jobs.ConfigPatch{
		{MaxAttempts: ptr(500)},
		{Timeout: ptr(48 * time.Hour)},
		{Priority: ptr(0)},
		{Schedule: ptr("@every 10s")},
		{Schedule: ptr("whenever")},
	}
	for _, patch := range invalid {
		_, err := s.manager.Update(ctx, "cleanup_sessions", patch, jobs.Change{Reason: "test"})
		var invalidErr *jobs.InvalidConfigError
		if !errors.As(err, &invalidErr) || !errors.Is(err, jobs.ErrInvalidConfig) {
			t.Errorf("Update(%+v) error = %v, want InvalidConfigError", patch, err)
		}
	}
	if _, err := s.manager.Update(ctx, "rebuild_index", jobs.ConfigPatch{Queue: ptr("reports")}, jobs.Change{}); !errors.Is(err, jobs.ErrUnknownQueue) {
		t.Errorf("Update(inactive queue) error = %v, want ErrUnknownQueue", err)
	}
	if _, err := s.manager.Update(context.Background(), "rebuild_index", jobs.ConfigPatch{MaxAttempts: ptr(3)}, jobs.Change{}); !errors.Is(err, jobs.ErrActorRequired) {
		t.Errorf("Update() without actor error = %v, want ErrActorRequired", err)
	}
	if _, err := s.manager.Update(ctx, "nope", jobs.ConfigPatch{}, jobs.Change{}); !errors.Is(err, jobs.ErrUnknownDefinition) {
		t.Errorf("Update(unknown) error = %v", err)
	}
	views, _ := s.manager.Definitions(ctx)
	for _, v := range views {
		if v.Version != 0 {
			t.Errorf("%s changed to version %d by rejected changes", v.Name, v.Version)
		}
	}
}

func TestRunNowUsesLiveConfiguration(t *testing.T) {
	ctx := operator()
	s := newManager(t, newPool(t))

	if _, err := s.manager.Update(ctx, "rebuild_index", jobs.ConfigPatch{MaxAttempts: ptr(3), Priority: ptr(2), Timeout: ptr(10 * time.Minute)}, jobs.Change{}); err != nil {
		t.Fatal(err)
	}
	run, err := s.manager.RunNow(ctx, "rebuild_index")
	if err != nil {
		t.Fatalf("RunNow() error = %v", err)
	}
	if run.MaxAttempts != 3 || run.Priority != 2 || run.Kind != "rebuild_index" || run.RequestID != "req_ops_1" || run.ActorID != "usr_ops" {
		t.Errorf("RunNow() = %+v, want live max attempts, priority and the operator's context", run)
	}
	got := receive(t, s.seen, "the job to run")
	if got.jobID != run.ID || got.timeout <= 9*time.Minute || got.timeout > 10*time.Minute {
		t.Errorf("worker saw job %d with timeout %v, want job %d with the live 10m timeout", got.jobID, got.timeout, run.ID)
	}

	// Jobs enqueued by application code use the live configuration too.
	res, err := s.client.Insert(context.Background(), rebuildArgs{}, nil)
	if err != nil || res.Job.MaxAttempts != 3 || res.Job.Priority != 2 {
		t.Errorf("code-enqueued job = %+v, %v; want max attempts 3, priority 2", res.Job, err)
	}

	waitFor(t, "the last run to show", func() bool {
		v, err := s.manager.Definition(ctx, "rebuild_index")
		return err == nil && v.LastRun != nil
	})
	if !slices.Contains(s.rec.actions(), "jobs.definition.run_requested") {
		t.Errorf("audit actions = %v, want jobs.definition.run_requested", s.rec.actions())
	}
}

func TestJobsListingRetryAndCancel(t *testing.T) {
	ctx := operator()
	s := newManager(t, newPool(t))

	later := &river.InsertOpts{ScheduledAt: time.Now().Add(time.Hour)}
	var ids []int64
	for range 3 {
		res, err := s.client.Insert(context.Background(), rebuildArgs{}, later)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, res.Job.ID)
	}

	first, err := s.manager.Jobs(ctx, jobs.JobFilter{Kind: "rebuild_index", Limit: 2})
	if err != nil || len(first.Jobs) != 2 || first.NextCursor == "" || first.Jobs[0].ID != ids[2] {
		t.Fatalf("Jobs(page 1) = %+v, %v; want the 2 newest and a cursor", first, err)
	}
	second, err := s.manager.Jobs(ctx, jobs.JobFilter{Kind: "rebuild_index", Limit: 2, Cursor: first.NextCursor})
	if err != nil || len(second.Jobs) != 1 || second.Jobs[0].ID != ids[0] || second.NextCursor != "" {
		t.Errorf("Jobs(page 2) = %+v, %v; want the oldest and no cursor", second, err)
	}
	if _, err := s.manager.Jobs(ctx, jobs.JobFilter{Cursor: "not-a-cursor"}); !errors.Is(err, jobs.ErrInvalidCursor) {
		t.Errorf("Jobs(bad cursor) error = %v", err)
	}
	scheduled, err := s.manager.Jobs(ctx, jobs.JobFilter{States: []rivertype.JobState{rivertype.JobStateScheduled}})
	if err != nil || len(scheduled.Jobs) != 3 {
		t.Errorf("Jobs(scheduled) = %d, %v; want 3", len(scheduled.Jobs), err)
	}

	cancelled, err := s.manager.Cancel(ctx, ids[0])
	if err != nil || cancelled.State != rivertype.JobStateCancelled {
		t.Errorf("Cancel() = %v, %v; want cancelled", cancelled.State, err)
	}
	retried, err := s.manager.Retry(ctx, ids[0])
	if err != nil || retried.State != rivertype.JobStateAvailable {
		t.Errorf("Retry() = %v, %v; want available", retried.State, err)
	}
	if _, err := s.manager.Job(ctx, 999_999_999); !errors.Is(err, jobs.ErrJobNotFound) {
		t.Errorf("Job(missing) error = %v, want ErrJobNotFound", err)
	}
	if _, err := s.manager.Cancel(context.Background(), ids[1]); !errors.Is(err, jobs.ErrActorRequired) {
		t.Errorf("Cancel() without actor error = %v", err)
	}
	actions := s.rec.actions()
	if !slices.Contains(actions, "jobs.run.cancelled") || !slices.Contains(actions, "jobs.run.retried") {
		t.Errorf("audit actions = %v", actions)
	}
}

func TestPauseAndResumeQueue(t *testing.T) {
	ctx := operator()
	s := newManager(t, newPool(t))

	if err := s.manager.PauseQueue(ctx, "default"); err != nil {
		t.Fatalf("PauseQueue() error = %v", err)
	}
	queues, err := s.manager.Queues(ctx)
	if err != nil || len(queues) != 1 || !queues[0].Paused || queues[0].PausedAt == nil {
		t.Errorf("Queues() after pause = %+v, %v", queues, err)
	}
	if err := s.manager.ResumeQueue(ctx, "default"); err != nil {
		t.Fatalf("ResumeQueue() error = %v", err)
	}
	if queues, _ := s.manager.Queues(ctx); queues[0].Paused {
		t.Error("queue still paused after ResumeQueue")
	}
	if err := s.manager.PauseQueue(ctx, "reports"); !errors.Is(err, jobs.ErrUnknownQueue) {
		t.Errorf("PauseQueue(inactive) error = %v, want ErrUnknownQueue", err)
	}
	if got := s.rec.actions(); !slices.Equal(got, []string{"jobs.queue.paused", "jobs.queue.resumed"}) {
		t.Errorf("audit actions = %v", got)
	}
}

func TestManagersConvergeAcrossInstances(t *testing.T) {
	ctx := operator()
	pool := newPool(t)
	a := newManager(t, pool)
	b := newManager(t, pool)
	run(t, b.manager)

	waitFor(t, "instance B to listen", func() bool {
		var n int
		_ = pool.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE query = 'LISTEN gorbital_jobs'").Scan(&n)
		return n > 0
	})
	if _, err := a.manager.Update(ctx, "cleanup_sessions", jobs.ConfigPatch{Enabled: ptr(false)}, jobs.Change{Reason: "maintenance"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "instance B to see the change", func() bool {
		v, err := b.manager.Definition(ctx, "cleanup_sessions")
		return err == nil && !v.Config.Enabled && v.Version == 1
	})
}
