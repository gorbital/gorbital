package repository

import (
	"context"
	"time"

	orgslib "gorbital.dev/modules/orgs"
)

// deleteOrgSQL removes only deleted organisations whose purge time has
// passed; foreign keys with ON DELETE CASCADE remove their members,
// invitations and org-scoped rows. The organisation's service accounts, in
// the auth module's table without a foreign key to orgs, go in the same
// statement, and their API keys with them (ADR-0058).
const deleteOrgSQL = `
	WITH purged AS (
		DELETE FROM orgs WHERE id = $1 AND deleted_at IS NOT NULL AND purge_after <= $2 RETURNING id
	), accounts AS (
		DELETE FROM auth_service_accounts WHERE org_id IN (SELECT id FROM purged)
	)
	SELECT count(*) FROM purged`

// DeleteOrg removes a deleted organisation past its purge time, with every
// org-scoped row, and reports whether it did.
func (s *Store) DeleteOrg(ctx context.Context, id orgslib.ID, now time.Time) (bool, error) {
	var n int
	err := s.db.QueryRow(ctx, deleteOrgSQL, id, now).Scan(&n)
	return n == 1, err
}
