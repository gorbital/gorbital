package repository

import (
	"context"
	"time"

	orgslib "gorbital.dev/modules/orgs"
)

const restoreOrgSQL = `
	UPDATE orgs SET deleted_at = NULL, purge_after = NULL, updated_at = $2, version = version + 1
	WHERE id = $1 AND deleted_at IS NOT NULL`

// RestoreOrg undoes MarkOrgDeleted.
func (s *Store) RestoreOrg(ctx context.Context, id orgslib.ID, now time.Time) error {
	_, err := s.db.Exec(ctx, restoreOrgSQL, id, now)
	return err
}
