package repository

import (
	"context"

	"example.com/shelfie/internal/modules/phonelogin/domain"
)

const insertCodeSQL = `
	INSERT INTO phone_codes (id, user_id, phone, purpose, code_hash, expires_at, created_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7)`

// InsertCode stores a code's hash.
func (s *Store) InsertCode(ctx context.Context, c domain.Code) error {
	_, err := s.db.Exec(ctx, insertCodeSQL, c.ID, c.UserID, c.Phone, c.Purpose, c.Hash, c.ExpiresAt, c.CreatedAt)
	return err
}
