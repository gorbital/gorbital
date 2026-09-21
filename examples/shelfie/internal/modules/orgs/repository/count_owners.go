package repository

import (
	"context"

	orgslib "gorbital.dev/modules/orgs"
)

const countOwnersSQL = `
	SELECT count(*) FROM org_members m JOIN auth_users u ON u.id = m.user_id
	WHERE m.org_id = $1 AND m.role = 'owner' AND m.user_id <> $2 AND u.deleted_at IS NULL`

// CountOwners returns how many owners other than excludeUserID an
// organisation has, counting only live accounts: a deleted account's
// membership stays until the account is purged.
func (s *Store) CountOwners(ctx context.Context, orgID orgslib.ID, excludeUserID string) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, countOwnersSQL, orgID, excludeUserID).Scan(&n)
	return n, err
}
