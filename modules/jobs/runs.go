package jobs

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"apistock.dev/audit"
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

// RunNow enqueues a job for an enabled definition immediately, with its
// current configuration. It returns [ErrUnknownDefinition],
// [ErrDefinitionDisabled] or [ErrActorRequired].
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
	res, err := m.client.Insert(ctx, d.newArgs(), nil)
	if err != nil {
		return JobRun{}, err
	}
	m.audit(ctx, audit.Event{
		Action:       "jobs.definition.run_requested",
		ResourceType: "job_definition",
		ResourceID:   name,
		Metadata:     map[string]any{"job_id": res.Job.ID},
	})
	return newJobRun(res.Job), nil
}

// Retry makes a job available to run again immediately. Running jobs are
// left alone. It returns [ErrJobNotFound] or [ErrActorRequired].
func (m *Manager) Retry(ctx context.Context, id int64) (JobRun, error) {
	return m.control(ctx, id, "jobs.run.retried", m.client.river.JobRetry)
}

// Cancel cancels a job: queued jobs never run, and a running job's context is
// cancelled. It returns [ErrJobNotFound] or [ErrActorRequired].
func (m *Manager) Cancel(ctx context.Context, id int64) (JobRun, error) {
	return m.control(ctx, id, "jobs.run.cancelled", m.client.river.JobCancel)
}

func (m *Manager) control(ctx context.Context, id int64, action string, op func(context.Context, int64) (*rivertype.JobRow, error)) (JobRun, error) {
	if _, err := requireActor(ctx); err != nil {
		return JobRun{}, err
	}
	row, err := op(ctx, id)
	if errors.Is(err, river.ErrNotFound) {
		return JobRun{}, ErrJobNotFound
	}
	if err != nil {
		return JobRun{}, fmt.Errorf("jobs: %s %d: %w", action, id, err)
	}
	m.audit(ctx, audit.Event{
		Action:       action,
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

// PauseQueue stops every instance fetching jobs from name. Running jobs
// finish. It returns [ErrUnknownQueue] or [ErrActorRequired].
func (m *Manager) PauseQueue(ctx context.Context, name string) error {
	return m.queueControl(ctx, name, "jobs.queue.paused", m.client.river.QueuePause)
}

// ResumeQueue resumes a paused queue on every instance.
func (m *Manager) ResumeQueue(ctx context.Context, name string) error {
	return m.queueControl(ctx, name, "jobs.queue.resumed", m.client.river.QueueResume)
}

func (m *Manager) queueControl(ctx context.Context, name, action string, op func(context.Context, string, *river.QueuePauseOpts) error) error {
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
	m.audit(ctx, audit.Event{Action: action, ResourceType: "job_queue", ResourceID: name})
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
