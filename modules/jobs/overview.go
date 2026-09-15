package jobs

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/riverqueue/river/rivertype"
)

// Overview summarises job work across every instance, for GET
// /ops/jobs/overview (ADR-0051).
type Overview struct {
	// Queues are the queues with active workers or unfinished jobs, by name.
	Queues []QueueOverview
	// Failing are the definitions whose most recent run failed: retrying or
	// discarded.
	Failing []DefinitionView
}

// QueueOverview counts one queue's unfinished jobs.
type QueueOverview struct {
	Name   string
	Paused bool
	// Active reports that some instance runs workers for the queue.
	Active                                   bool
	Available, Scheduled, Running, Retryable int64
	// DiscardedLastDay counts jobs that ran out of attempts in the last 24
	// hours.
	DiscardedLastDay int64
}

// overviewTimeout bounds the overview's queries.
const overviewTimeout = 5 * time.Second

// The state index River fetches jobs with serves the unfinished states.
const selectQueueCountsSQL = `
	SELECT queue, state::text, count(*)
	FROM river_job
	WHERE state IN ('available', 'scheduled', 'running', 'retryable')
	   OR (state = 'discarded' AND finalized_at >= $1)
	GROUP BY queue, state`

// Overview returns the job overview.
func (m *Manager) Overview(ctx context.Context) (Overview, error) {
	ctx, cancel := context.WithTimeout(ctx, overviewTimeout)
	defer cancel()

	byName := map[string]*QueueOverview{}
	queue := func(name string) *QueueOverview {
		q, ok := byName[name]
		if !ok {
			q = &QueueOverview{Name: name}
			byName[name] = q
		}
		return q
	}

	rows, err := m.pool.Query(ctx, selectQueueCountsSQL, m.now().Add(-24*time.Hour))
	if err != nil {
		return Overview{}, fmt.Errorf("jobs: count jobs: %w", err)
	}
	for rows.Next() {
		var (
			name, state string
			n           int64
		)
		if err := rows.Scan(&name, &state, &n); err != nil {
			rows.Close()
			return Overview{}, fmt.Errorf("jobs: count jobs: %w", err)
		}
		q := queue(name)
		switch rivertype.JobState(state) {
		case rivertype.JobStateAvailable:
			q.Available = n
		case rivertype.JobStateScheduled:
			q.Scheduled = n
		case rivertype.JobStateRunning:
			q.Running = n
		case rivertype.JobStateRetryable:
			q.Retryable = n
		case rivertype.JobStateDiscarded:
			q.DiscardedLastDay = n
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Overview{}, fmt.Errorf("jobs: count jobs: %w", err)
	}

	active, err := m.Queues(ctx)
	if err != nil {
		return Overview{}, err
	}
	for _, a := range active {
		q := queue(a.Name)
		q.Active, q.Paused = true, a.Paused
	}

	defs, err := m.Definitions(ctx)
	if err != nil {
		return Overview{}, err
	}
	o := Overview{Queues: make([]QueueOverview, 0, len(byName)), Failing: []DefinitionView{}}
	for _, q := range byName {
		o.Queues = append(o.Queues, *q)
	}
	slices.SortFunc(o.Queues, func(a, b QueueOverview) int { return cmp.Compare(a.Name, b.Name) })
	for _, d := range defs {
		if d.LastRun != nil && (d.LastRun.State == rivertype.JobStateRetryable || d.LastRun.State == rivertype.JobStateDiscarded) {
			o.Failing = append(o.Failing, d)
		}
	}
	return o, nil
}
