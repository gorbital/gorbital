package jobs

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"gorbital.dev/audit"
)

// JobRun is one job and its attempts, without its arguments: arguments can
// contain personal data and are never shown in the admin panel.
type JobRun struct {
	ID          int64
	Kind        string
	Queue       string
	State       rivertype.JobState
	Attempt     int
	MaxAttempts int
	Priority    int
	CreatedAt   time.Time
	ScheduledAt time.Time
	AttemptedAt *time.Time
	FinalizedAt *time.Time
	Errors      []AttemptError
	// RequestID and the enqueuing actor come from the job's context metadata.
	RequestID string
	ActorKind string
	ActorID   string
}

// AttemptError is a failed attempt. Stack traces are omitted; they are in
// the logs.
type AttemptError struct {
	At      time.Time
	Attempt int
	Message string
}

func newJobRun(row *rivertype.JobRow) JobRun {
	jc := readJobContext(row.Metadata)
	run := JobRun{
		ID:          row.ID,
		Kind:        row.Kind,
		Queue:       row.Queue,
		State:       row.State,
		Attempt:     row.Attempt,
		MaxAttempts: row.MaxAttempts,
		Priority:    row.Priority,
		CreatedAt:   row.CreatedAt,
		ScheduledAt: row.ScheduledAt,
		AttemptedAt: row.AttemptedAt,
		FinalizedAt: row.FinalizedAt,
		RequestID:   jc.RequestID,
		ActorKind:   jc.ActorKind,
		ActorID:     jc.ActorID,
	}
	for _, e := range row.Errors {
		run.Errors = append(run.Errors, AttemptError{At: e.At, Attempt: e.Attempt, Message: e.Error})
	}
	return run
}

// JobFilter selects jobs to list. Empty fields match everything.
type JobFilter struct {
	Kind   string
	Queue  string
	States []rivertype.JobState
	// Limit is clamped to 1–100; 0 means 50.
	Limit int
	// Cursor is JobPage.NextCursor from the previous page.
	Cursor string
}

// JobPage is a page of jobs, newest first.
type JobPage struct {
	Jobs []JobRun
	// NextCursor fetches the next page; empty on the last page.
	NextCursor string
}

// Jobs lists jobs, newest first. It returns [ErrInvalidCursor] for a
// malformed cursor.
func (m *Manager) Jobs(ctx context.Context, f JobFilter) (JobPage, error) {
	limit := f.Limit
	if limit == 0 {
		limit = 50
	}
	limit = min(max(limit, 1), 100)
	params := river.NewJobListParams().First(limit).OrderBy(river.JobListOrderByID, river.SortOrderDesc)
	if f.Kind != "" {
		params = params.Kinds(f.Kind)
	}
	if f.Queue != "" {
		params = params.Queues(f.Queue)
	}
	if len(f.States) > 0 {
		params = params.States(f.States...)
	}
	if f.Cursor != "" {
		var cursor river.JobListCursor
		if err := cursor.UnmarshalText([]byte(f.Cursor)); err != nil {
			return JobPage{}, ErrInvalidCursor
		}
		params = params.After(&cursor)
	}
	res, err := m.client.river.JobList(ctx, params)
	if err != nil {
		return JobPage{}, fmt.Errorf("jobs: list jobs: %w", err)
	}
	page := JobPage{Jobs: make([]JobRun, len(res.Jobs))}
	for i, row := range res.Jobs {
		page.Jobs[i] = newJobRun(row)
	}
	if len(res.Jobs) == limit && res.LastCursor != nil {
		text, err := res.LastCursor.MarshalText()
		if err != nil {
			return JobPage{}, fmt.Errorf("jobs: encode cursor: %w", err)
		}
		page.NextCursor = string(text)
	}
	return page, nil
}

// Job returns one job, or [ErrJobNotFound].
func (m *Manager) Job(ctx context.Context, id int64) (JobRun, error) {
	row, err := m.client.river.JobGet(ctx, id)
	if errors.Is(err, river.ErrNotFound) {
		return JobRun{}, ErrJobNotFound
	}
	if err != nil {
		return JobRun{}, fmt.Errorf("jobs: get job %d: %w", id, err)
	}
	return newJobRun(row), nil
}

func (m *Manager) lastRun(ctx context.Context, kind string) (*JobRun, error) {
	page, err := m.Jobs(ctx, JobFilter{Kind: kind, Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(page.Jobs) == 0 {
		return nil, nil
	}
	return &page.Jobs[0], nil
}

// runNowStates are the states in which an on-demand run blocks another:
// River refuses the insert atomically, so concurrent requests queue one run.
var runNowStates = []rivertype.JobState{
	rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRetryable,
	rivertype.JobStateRunning, rivertype.JobStateScheduled,
}

// RunNow enqueues a job for an enabled definition immediately, with its
// current configuration. A definition runs on demand at most once at a
// time and once per [MinScheduleInterval], the same floor as schedules. It
// returns [ErrUnknownDefinition], [ErrDefinitionDisabled], [ErrRunLimited]
// or [ErrActorRequired].
func (m *Manager) RunNow(ctx context.Context, name string) (JobRun, error) {
	d, ok := m.defs.lookup(name)
	if !ok {
		return JobRun{}, ErrUnknownDefinition
	}
	if _, err := requireActor(ctx); err != nil {
		return JobRun{}, err
	}
	if !m.defs.effective(d).Enabled {
		return JobRun{}, ErrDefinitionDisabled
	}
	last, err := m.lastRun(ctx, name)
	if err != nil {
		return JobRun{}, err
	}
	if last != nil && (inProgress(last.State) || m.now().Sub(last.CreatedAt) < MinScheduleInterval) {
		return JobRun{}, ErrRunLimited
	}
	res, err := m.client.Insert(ctx, d.newArgs(), &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByState: runNowStates}})
	if err != nil {
		return JobRun{}, err
	}
	if res.UniqueSkippedAsDuplicate {
		return JobRun{}, ErrRunLimited
	}
	m.audit(ctx, audit.Event{
		Action:       "jobs.definition.run_requested",
		ResourceType: "job_definition",
		ResourceID:   name,
		Metadata:     map[string]any{"job_id": res.Job.ID},
	})
	return newJobRun(res.Job), nil
}

// inProgress reports a job that is queued to run now or running.
func inProgress(state rivertype.JobState) bool {
	switch state {
	case rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRetryable, rivertype.JobStateRunning:
		return true
	}
	return false
}

// retryableStates are the states [Manager.Retry] accepts.
var retryableStates = []rivertype.JobState{
	rivertype.JobStateRetryable, rivertype.JobStateDiscarded, rivertype.JobStateCancelled,
}

// Retry makes a job waiting to retry, discarded or cancelled available to
// run again immediately. Completed, queued and running jobs are refused, so
// a delivered email or a finished purge never runs twice, and so are jobs of
// a disabled definition. It returns [ErrJobNotFound], [ErrJobNotRetryable],
// [ErrDefinitionDisabled] or [ErrActorRequired].
func (m *Manager) Retry(ctx context.Context, id int64) (JobRun, error) {
	if _, err := requireActor(ctx); err != nil {
		return JobRun{}, err
	}
	var row *rivertype.JobRow
	err := pgx.BeginFunc(ctx, m.pool, func(tx pgx.Tx) error {
		kind, state, err := selectJobForRetry(ctx, tx, id)
		if err != nil {
			return err
		}
		if !slices.Contains(retryableStates, state) {
			return ErrJobNotRetryable
		}
		if d, ok := m.defs.lookup(kind); ok && !m.defs.effective(d).Enabled {
			return ErrDefinitionDisabled
		}
		row, err = m.client.river.JobRetryTx(ctx, tx, id)
		return err
	})
	switch {
	case errors.Is(err, ErrJobNotFound), errors.Is(err, river.ErrNotFound):
		return JobRun{}, ErrJobNotFound
	case errors.Is(err, ErrJobNotRetryable), errors.Is(err, ErrDefinitionDisabled):
		return JobRun{}, err
	case err != nil:
		return JobRun{}, fmt.Errorf("jobs: retry %d: %v", id, err) //nolint:errorlint // driver errors aren't API (ADR-0018)
	}
	m.audit(ctx, audit.Event{
		Action:       "jobs.run.retried",
		ResourceType: "job",
		ResourceID:   strconv.FormatInt(id, 10),
		Metadata:     map[string]any{"kind": row.Kind, "state": string(row.State)},
	})
	return newJobRun(row), nil
}

// Cancel cancels a job: queued jobs never run, and a running job's context is
// cancelled. It returns [ErrJobNotFound] or [ErrActorRequired].
func (m *Manager) Cancel(ctx context.Context, id int64) (JobRun, error) {
	if _, err := requireActor(ctx); err != nil {
		return JobRun{}, err
	}
	row, err := m.client.river.JobCancel(ctx, id)
	if errors.Is(err, river.ErrNotFound) {
		return JobRun{}, ErrJobNotFound
	}
	if err != nil {
		return JobRun{}, fmt.Errorf("jobs: cancel %d: %w", id, err)
	}
	m.audit(ctx, audit.Event{
		Action:       "jobs.run.cancelled",
		ResourceType: "job",
		ResourceID:   strconv.FormatInt(id, 10),
		Metadata:     map[string]any{"kind": row.Kind, "state": string(row.State)},
	})
	return newJobRun(row), nil
}

// Queue is a queue workers run or recently ran.
type Queue struct {
	Name      string
	Paused    bool
	PausedAt  *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Queues lists active queues by name.
func (m *Manager) Queues(ctx context.Context) ([]Queue, error) {
	res, err := m.client.river.QueueList(ctx, river.NewQueueListParams().First(1000))
	if err != nil {
		return nil, fmt.Errorf("jobs: list queues: %w", err)
	}
	queues := make([]Queue, len(res.Queues))
	for i, q := range res.Queues {
		queues[i] = Queue{Name: q.Name, Paused: q.PausedAt != nil, PausedAt: q.PausedAt, CreatedAt: q.CreatedAt, UpdatedAt: q.UpdatedAt}
	}
	slices.SortFunc(queues, func(a, b Queue) int { return cmpString(a.Name, b.Name) })
	return queues, nil
}

// PauseQueue always returns [ErrReasonRequired]: pausing a queue needs a
// reason.
//
// Deprecated: Use [Manager.PauseQueueWithReason].
func (m *Manager) PauseQueue(ctx context.Context, name string) error {
	return m.PauseQueueWithReason(ctx, name, "")
}

// PauseQueueWithReason stops every instance fetching jobs from name, and
// records reason in the audit event. Running jobs finish. Pausing stops
// every job in the queue, email delivery included, so it needs a reason. It
// returns [ErrReasonRequired], [ErrUnknownQueue] or [ErrActorRequired].
func (m *Manager) PauseQueueWithReason(ctx context.Context, name, reason string) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		if _, err := requireActor(ctx); err != nil {
			return err
		}
		return ErrReasonRequired
	}
	return m.queueControl(ctx, name, reason, "jobs.queue.paused", m.client.river.QueuePause)
}

// ResumeQueue resumes a paused queue on every instance.
func (m *Manager) ResumeQueue(ctx context.Context, name string) error {
	return m.ResumeQueueWithReason(ctx, name, "")
}

// ResumeQueueWithReason resumes a paused queue on every instance, recording
// reason, which may be empty, in the audit event.
func (m *Manager) ResumeQueueWithReason(ctx context.Context, name, reason string) error {
	return m.queueControl(ctx, name, strings.TrimSpace(reason), "jobs.queue.resumed", m.client.river.QueueResume)
}

func (m *Manager) queueControl(ctx context.Context, name, reason, action string, op func(context.Context, string, *river.QueuePauseOpts) error) error {
	if _, err := requireActor(ctx); err != nil {
		return err
	}
	if err := m.requireActiveQueue(ctx, name); err != nil {
		return err
	}
	if err := op(ctx, name, nil); err != nil {
		if errors.Is(err, river.ErrNotFound) {
			return ErrUnknownQueue
		}
		return fmt.Errorf("jobs: %s %s: %w", action, name, err)
	}
	e := audit.Event{Action: action, ResourceType: "job_queue", ResourceID: name}
	if reason != "" {
		e.Metadata = map[string]any{"reason": reason}
	}
	m.audit(ctx, e)
	return nil
}

func (m *Manager) requireActiveQueue(ctx context.Context, name string) error {
	queues, err := m.Queues(ctx)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(queues, func(q Queue) bool { return q.Name == name }) {
		return ErrUnknownQueue
	}
	return nil
}

func sortByNextRun(views []DefinitionView) {
	slices.SortStableFunc(views, func(a, b DefinitionView) int { return a.NextRunAt.Compare(b.NextRunAt) })
}

func cmpString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
