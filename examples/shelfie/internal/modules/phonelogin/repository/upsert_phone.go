package repository

import (
	"context"
	"time"
)

const upsertPhoneSQL = `
	INSERT INTO phone_numbers (user_id, phone, created_at) VALUES ($1, $2, $3)
	ON CONFLICT (user_id) DO UPDATE SET phone = EXCLUDED.phone, confirmed_at = NULL, created_at = EXCLUDED.created_at`

// UpsertPhone sets a reader's number, unconfirmed.
func (s *Store) UpsertPhone(ctx context.Context, userID, phone string, now time.Time) error {
	_, err := s.db.Exec(ctx, upsertPhoneSQL, userID, phone, now)
	return err
}
