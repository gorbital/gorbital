package repository

import (
	"context"
	"time"
)

const selectCodesSinceSQL = `
	SELECT count(*), COALESCE(max(created_at), 'epoch'::timestamptz) FROM phone_codes
	WHERE phone = $1 AND purpose = $2 AND created_at >= $3`

// SelectCodesSince returns how many codes of a purpose were sent to phone
// since since, and when the newest was.
func (s *Store) SelectCodesSince(ctx context.Context, phone, purpose string, since time.Time) (int, time.Time, error) {
	var (
		n    int
		last time.Time
	)
	err := s.db.QueryRow(ctx, selectCodesSinceSQL, phone, purpose, since).Scan(&n, &last)
	return n, last, err
}
