package repository

import (
	"context"

	orgslib "gorbital.dev/modules/orgs"
	"gorbital.dev/modules/postgres"
)

const memberRoleSQL = `
	SELECT m.role FROM org_members m JOIN orgs o ON o.id = m.org_id
	WHERE m.org_id = $1 AND m.user_id = $2 AND o.deleted_at IS NULL`

// MemberRole returns userID's role in a live organisation, or
// orgs.ErrNotMember. orgs.RequireMember calls it on every organisation
// request.
func (s *Store) MemberRole(ctx context.Context, orgID orgslib.ID, userID string) (string, error) {
	var role string
	err := s.db.QueryRow(ctx, memberRoleSQL, orgID, userID).Scan(&role)
	if postgres.IsNoRows(err) {
		return "", orgslib.ErrNotMember
	}
	return role, err
}
