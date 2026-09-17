package repository

import (
	"context"
	"time"
)

const countActiveSQL = `SELECT count(*) FROM announcements WHERE starts_at <= $1 AND ends_at > $1`

// CountActive returns how many announcements are active at now.
func (s *Store) CountActive(ctx context.Context, now time.Time) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, countActiveSQL, now).Scan(&n)
	return n, err
}
