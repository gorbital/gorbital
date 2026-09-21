package repository

import (
	"context"
	"time"

	"gorbital.dev/modules/postgres"

	"example.com/shelfie/internal/modules/phonelogin/domain"
)

const confirmPhoneSQL = `UPDATE phone_numbers SET confirmed_at = $3 WHERE user_id = $1 AND phone = $2`

// ConfirmPhone confirms a reader's number, or returns ErrPhoneTaken when
// another account confirmed it first.
func (s *Store) ConfirmPhone(ctx context.Context, userID, phone string, now time.Time) error {
	tag, err := s.db.Exec(ctx, confirmPhoneSQL, userID, phone, now)
	if _, ok := postgres.UniqueViolation(err); ok {
		return domain.ErrPhoneTaken
	}
	if err == nil && tag.RowsAffected() == 0 {
		return domain.ErrInvalidCode // the reader changed their number since
	}
	return err
}
