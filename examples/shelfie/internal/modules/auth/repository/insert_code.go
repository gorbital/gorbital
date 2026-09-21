package repository

import (
	"context"

	authdomain "example.com/shelfie/internal/modules/auth/domain"
)

const insertCodeSQL = `
	INSERT INTO auth_codes (id, user_id, purpose, code_hash, max_attempts, expires_at, created_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7)`

// InsertCode stores a code's hash.
func (s *Store) InsertCode(ctx context.Context, c authdomain.Code) error {
	_, err := s.db.Exec(ctx, insertCodeSQL, c.ID, c.UserID, c.Purpose, c.Hash, c.MaxAttempts, c.ExpiresAt, c.CreatedAt)
	return err
}
