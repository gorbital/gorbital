package repository

import (
	"context"
	"time"
)

// docs:start delete-expired

// deleteExpiredSQL deletes one batch: PostgreSQL's DELETE has no LIMIT, so
// the subquery picks the rows. The retention job repeats it until a batch
// is short, so no statement holds locks on every expired row at once.
const deleteExpiredSQL = `
	DELETE FROM announcements
	WHERE id IN (SELECT id FROM announcements WHERE ends_at < $1 LIMIT $2)`

// DeleteExpired removes up to limit announcements that ended before before.
func (s *Store) DeleteExpired(ctx context.Context, before time.Time, limit int) (int64, error) {
	tag, err := s.db.Exec(ctx, deleteExpiredSQL, before, limit)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// docs:end delete-expired
