package repository

import (
	"context"

	orgslib "gorbital.dev/modules/orgs"
	"gorbital.dev/modules/postgres"
)

// An organisation's service account (ADR-0058, in the auth module's
// auth_service_accounts table) holds exactly one role; a disabled one, or
// one of a deleted organisation, holds none.
const serviceAccountRoleSQL = `
	SELECT a.roles[1] FROM auth_service_accounts a JOIN orgs o ON o.id = a.org_id
	WHERE a.org_id = $1 AND a.id = $2 AND a.disabled_at IS NULL AND cardinality(a.roles) = 1 AND o.deleted_at IS NULL`

// ServiceAccountRole returns the role of an enabled service account of a
// live organisation, or orgs.ErrNotMember.
func (s *Store) ServiceAccountRole(ctx context.Context, orgID orgslib.ID, serviceAccountID string) (string, error) {
	var role string
	err := s.db.QueryRow(ctx, serviceAccountRoleSQL, orgID, serviceAccountID).Scan(&role)
	if postgres.IsNoRows(err) {
		return "", orgslib.ErrNotMember
	}
	return role, err
}
