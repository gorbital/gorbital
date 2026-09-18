package repository

import "context"

const countIdentitiesSQL = `SELECT count(*) FROM auth_identities WHERE user_id = $1`

// CountIdentities returns how many identities a user has linked.
func (s *Store) CountIdentities(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, countIdentitiesSQL, userID).Scan(&n)
	return n, err
}
