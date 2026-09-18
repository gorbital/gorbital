package repository

import (
	"context"

	orgslib "gorbital.dev/modules/orgs"

	orgsdomain "example.com/plateful/internal/modules/orgs/domain"
)

const deleteMemberSQL = `DELETE FROM org_members WHERE org_id = $1 AND user_id = $2`

// DeleteMember removes a member, or returns ErrMemberNotFound.
func (s *Store) DeleteMember(ctx context.Context, orgID orgslib.ID, userID string) error {
	tag, err := s.db.Exec(ctx, deleteMemberSQL, orgID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return orgsdomain.ErrMemberNotFound
	}
	return nil
}
