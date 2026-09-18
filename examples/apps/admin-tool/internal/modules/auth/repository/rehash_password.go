package repository

import "context"

//nolint:gosec // SQL text, not a credential
const rehashPasswordSQL = `UPDATE auth_users SET password_hash = $2 WHERE id = $1`

// RehashPassword upgrades a hash's parameters; the password stays the same.
func (s *Store) RehashPassword(ctx context.Context, userID, passwordHash string) error {
	_, err := s.db.Exec(ctx, rehashPasswordSQL, userID, passwordHash)
	return err
}
