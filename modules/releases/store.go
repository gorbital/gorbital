package releases

import (
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Store queries recorded instances and the releases they ran. It is safe for
// concurrent use.
type Store struct {
	pool *pgxpool.Pool
	cfg  config
}

// NewStore returns a store on pool. Give it the trackers' heartbeat, if not
// the default.
func NewStore(pool *pgxpool.Pool, opts ...Option) (*Store, error) {
	cfg, err := newConfig(opts)
	if pool == nil {
		err = errors.Join(err, errors.New("pool is required"))
	}
	if err != nil {
		return nil, fmt.Errorf("releases: invalid store: %w", err)
	}
	return &Store{pool: pool, cfg: cfg}, nil
}

// runningSince is the oldest last heartbeat an instance can have and still
// count as running.
func (s *Store) runningSince() time.Time {
	return s.cfg.now().UTC().Add(-runningHeartbeats * s.cfg.heartbeat)
}

func (s *Store) markRunning(instances []Instance) {
	since := s.runningSince()
	for i := range instances {
		instances[i].Running = instances[i].StoppedAt == nil && !instances[i].LastSeenAt.Before(since)
	}
}

// pageLimit clamps a requested page size to 1–100; 0 means 50.
func pageLimit(n int) int {
	if n == 0 {
		return 50
	}
	return min(max(n, 1), 100)
}
