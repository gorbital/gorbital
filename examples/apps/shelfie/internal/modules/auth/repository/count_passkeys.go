package repository

import "context"

//nolint:gosec // SQL text, not a credential
const countPasskeysSQL = `SELECT count(*) FROM auth_passkeys WHERE user_id = $1`

// CountPasskeys returns how many passkeys a user has.
func (s *Store) CountPasskeys(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, countPasskeysSQL, userID).Scan(&n)
	return n, err
}
