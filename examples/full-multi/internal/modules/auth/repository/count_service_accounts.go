package repository

import "context"

const countServiceAccountsSQL = `SELECT count(*) FROM auth_service_accounts WHERE org_id IS NOT DISTINCT FROM NULLIF($1, '')`

// CountServiceAccounts returns how many service accounts an organisation,
// or the platform for an empty orgID, has.
func (s *Store) CountServiceAccounts(ctx context.Context, orgID string) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, countServiceAccountsSQL, orgID).Scan(&n)
	return n, err
}
