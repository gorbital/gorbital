package repository

import (
	"context"

	orgslib "apistock.dev/modules/orgs"
)

// deleteOrgSQL removes only deleted organisations; foreign keys with ON
// DELETE CASCADE remove their members, invitations and org-scoped rows.
const deleteOrgSQL = `DELETE FROM orgs WHERE id = $1 AND deleted_at IS NOT NULL`

// DeleteOrg removes a deleted organisation and every org-scoped row.
func (s *Store) DeleteOrg(ctx context.Context, id orgslib.ID) error {
	_, err := s.db.Exec(ctx, deleteOrgSQL, id)
	return err
}
