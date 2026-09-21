package repository

import "context"

const countOwnedOrgsSQL = `
	SELECT count(*) FROM org_members m JOIN orgs o ON o.id = m.org_id
	WHERE m.user_id = $1 AND m.role = 'owner' AND NOT o.personal AND o.deleted_at IS NULL`

// CountOwnedOrgs returns how many live organisations userID owns, personal
// workspaces aside.
func (s *Store) CountOwnedOrgs(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, countOwnedOrgsSQL, userID).Scan(&n)
	return n, err
}
