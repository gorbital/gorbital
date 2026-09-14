package jobs

import (
	"context"
	"fmt"
	"maps"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

// applyDefinitions sets queue, priority and max attempts from each defined
// job's live configuration at insert time, so admin-panel changes apply to
// every job enqueued afterwards, including jobs enqueued by application code.
type applyDefinitions struct {
	river.MiddlewareDefaults
	defs *Definitions
}

var _ rivertype.JobInsertMiddleware = (*applyDefinitions)(nil)

func (m *applyDefinitions) InsertMany(ctx context.Context, params []*rivertype.JobInsertParams,
	doInner func(context.Context) ([]*rivertype.JobInsertResult, error),
) ([]*rivertype.JobInsertResult, error) {
	for _, p := range params {
		if cfg, ok := m.defs.configFor(p.Kind); ok {
			p.Queue, p.Priority, p.MaxAttempts = cfg.Queue, cfg.Priority, cfg.MaxAttempts
		}
	}
	return doInner(ctx)
}

// scheduler keeps River's periodic jobs in line with definition schedules.
// Every working instance applies the same schedules; River's elected leader
// is the only one that inserts them.
type scheduler struct {
	mu      sync.Mutex
	applied map[string]string // definition name → schedule currently registered
}

// reconcile adds, replaces or removes periodic jobs whose effective schedule
// changed since the last call.
func (s *scheduler) reconcile(rc *river.Client[pgx.Tx], defs *Definitions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.applied == nil {
		s.applied = make(map[string]string)
	}
	bundle := rc.PeriodicJobs()
	next := maps.Clone(s.applied)
	for _, d := range defs.list() {
		cfg := defs.effective(d)
		want := ""
		if cfg.Enabled {
			want = cfg.Schedule
		}
		if next[d.name] == want {
			continue
		}
		bundle.RemoveByID(d.name)
		delete(next, d.name)
		if want == "" {
			continue
		}
		schedule, err := parseSchedule(want)
		if err != nil {
			return fmt.Errorf("jobs: schedule %s: %w", d.name, err)
		}
		d := d
		constructor := func() (river.JobArgs, *river.InsertOpts) {
			// A job disabled between reconciliations must not fire.
			if !defs.effective(d).Enabled {
				return nil, nil
			}
			return d.newArgs(), nil
		}
		if _, err := bundle.AddSafely(river.NewPeriodicJob(schedule, constructor, &river.PeriodicJobOpts{ID: d.name})); err != nil {
			return fmt.Errorf("jobs: schedule %s: %w", d.name, err)
		}
		next[d.name] = want
	}
	s.applied = next
	return nil
}
