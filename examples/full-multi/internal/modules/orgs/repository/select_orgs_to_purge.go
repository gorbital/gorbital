package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	orgslib "gorbital.dev/modules/orgs"
)

const selectOrgsToPurgeSQL = `
	SELECT id FROM orgs WHERE deleted_at IS NOT NULL AND purge_after <= $1
	ORDER BY purge_after, id LIMIT $2`

// SelectOrgsToPurge returns up to limit organisations whose purge time has
// passed.
func (s *Store) SelectOrgsToPurge(ctx context.Context, now time.Time, limit int) ([]orgslib.ID, error) {
	rows, err := s.db.Query(ctx, selectOrgsToPurgeSQL, now, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (orgslib.ID, error) {
		var id string
		err := row.Scan(&id)
		return orgslib.ID(id), err
	})
}
