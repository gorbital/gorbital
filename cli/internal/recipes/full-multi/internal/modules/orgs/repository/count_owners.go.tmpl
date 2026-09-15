package repository

import (
	"context"

	orgslib "apistock.dev/modules/orgs"
)

const countOwnersSQL = `SELECT count(*) FROM org_members WHERE org_id = $1 AND role = 'owner'`

// CountOwners returns how many owners an organisation has.
func (s *Store) CountOwners(ctx context.Context, orgID orgslib.ID) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, countOwnersSQL, orgID).Scan(&n)
	return n, err
}
