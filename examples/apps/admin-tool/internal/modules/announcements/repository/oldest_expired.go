package repository

import (
	"context"
	"time"
)

const oldestExpiredSQL = `SELECT min(ends_at) FROM announcements WHERE ends_at <= $1`

// OldestExpired returns the earliest end among the announcements that ended
// at or before now, and false when none has.
func (s *Store) OldestExpired(ctx context.Context, now time.Time) (time.Time, bool, error) {
	var oldest *time.Time
	if err := s.db.QueryRow(ctx, oldestExpiredSQL, now).Scan(&oldest); err != nil || oldest == nil {
		return time.Time{}, false, err
	}
	return oldest.UTC(), true, nil
}
