package repository

import (
	"context"
	"time"
)

const consumeCodesSQL = `
	UPDATE auth_codes SET consumed_at = $3
	WHERE user_id = $1 AND purpose = $2 AND consumed_at IS NULL`

// ConsumeCodes ends every unused code of a purpose, so only the newest code
// works.
func (s *Store) ConsumeCodes(ctx context.Context, userID, purpose string, now time.Time) error {
	_, err := s.db.Exec(ctx, consumeCodesSQL, userID, purpose, now)
	return err
}
