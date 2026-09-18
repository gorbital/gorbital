package repository

import (
	"context"
	"time"

	orgslib "gorbital.dev/modules/orgs"
)

const markOrgDeletedSQL = `
	UPDATE orgs SET deleted_at = $2, purge_after = $3, updated_at = $2, version = version + 1
	WHERE id = $1 AND deleted_at IS NULL`

// MarkOrgDeleted soft deletes an organisation until purgeAfter.
func (s *Store) MarkOrgDeleted(ctx context.Context, id orgslib.ID, now, purgeAfter time.Time) error {
	_, err := s.db.Exec(ctx, markOrgDeletedSQL, id, now, purgeAfter)
	return err
}
