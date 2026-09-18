package repository

import (
	"context"

	orgslib "gorbital.dev/modules/orgs"

	orgsdomain "example.com/invoicing/internal/modules/orgs/domain"
)

const updateMemberRoleSQL = `UPDATE org_members SET role = $3 WHERE org_id = $1 AND user_id = $2`

// UpdateMemberRole changes a member's role, or returns ErrMemberNotFound.
func (s *Store) UpdateMemberRole(ctx context.Context, orgID orgslib.ID, userID, role string) error {
	tag, err := s.db.Exec(ctx, updateMemberRoleSQL, orgID, userID, role)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return orgsdomain.ErrMemberNotFound
	}
	return nil
}
