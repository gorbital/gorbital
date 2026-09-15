package repository

import (
	"context"
	"time"

	orgslib "gorbital.dev/modules/orgs"
)

// deleteOrgSQL removes only deleted organisations whose purge time has
// passed; foreign keys with ON DELETE CASCADE remove their members,
// invitations and org-scoped rows.
const deleteOrgSQL = `DELETE FROM orgs WHERE id = $1 AND deleted_at IS NOT NULL AND purge_after <= $2`

// DeleteOrg removes a deleted organisation past its purge time, with every
// org-scoped row, and reports whether it did.
func (s *Store) DeleteOrg(ctx context.Context, id orgslib.ID, now time.Time) (bool, error) {
	tag, err := s.db.Exec(ctx, deleteOrgSQL, id, now)
	return tag.RowsAffected() == 1, err
}
