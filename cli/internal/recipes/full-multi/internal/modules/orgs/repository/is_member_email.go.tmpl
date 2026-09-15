package repository

import (
	"context"

	orgslib "gorbital.dev/modules/orgs"
)

const isMemberEmailSQL = `
	SELECT EXISTS (
		SELECT 1 FROM org_members m JOIN auth_users u ON u.id = m.user_id
		WHERE m.org_id = $1 AND u.email_normalized = $2 AND u.email_verified_at IS NOT NULL
	)`

// IsMemberEmail reports whether a user with this normalized, verified email
// address is a member.
func (s *Store) IsMemberEmail(ctx context.Context, orgID orgslib.ID, normalizedEmail string) (bool, error) {
	var member bool
	err := s.db.QueryRow(ctx, isMemberEmailSQL, orgID, normalizedEmail).Scan(&member)
	return member, err
}
