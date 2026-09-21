package repository

import (
	"context"
	"time"

	"gorbital.dev/modules/postgres"

	"example.com/shelfie/internal/modules/phonelogin/domain"
)

const selectActiveCodeSQL = `
	SELECT id, user_id, phone, purpose, code_hash, attempts, expires_at, created_at FROM phone_codes
	WHERE phone = $1 AND purpose = $2 AND used_at IS NULL AND expires_at > $3 AND attempts < $4
	ORDER BY created_at DESC LIMIT 1
	FOR UPDATE`

// SelectActiveCode locks the newest usable code of a purpose for phone.
func (s *Store) SelectActiveCode(ctx context.Context, phone, purpose string, now time.Time) (domain.Code, bool, error) {
	var c domain.Code
	err := s.db.QueryRow(ctx, selectActiveCodeSQL, phone, purpose, now, domain.CodeAttempts).
		Scan(&c.ID, &c.UserID, &c.Phone, &c.Purpose, &c.Hash, &c.Attempts, &c.ExpiresAt, &c.CreatedAt)
	if postgres.IsNoRows(err) {
		return domain.Code{}, false, nil
	}
	return c, err == nil, err
}
