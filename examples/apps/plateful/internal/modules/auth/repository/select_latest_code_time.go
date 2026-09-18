package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

const selectLatestCodeTimeSQL = `
	SELECT created_at FROM auth_codes
	WHERE user_id = $1 AND purpose = $2
	ORDER BY created_at DESC LIMIT 1`

// SelectLatestCodeTime returns when the newest code of a purpose was issued.
func (s *Store) SelectLatestCodeTime(ctx context.Context, userID, purpose string) (time.Time, bool, error) {
	var t time.Time
	err := s.db.QueryRow(ctx, selectLatestCodeTimeSQL, userID, purpose).Scan(&t)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, false, nil
	}
	return t, err == nil, err
}
