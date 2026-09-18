package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	orgslib "gorbital.dev/modules/orgs"
	"gorbital.dev/modules/postgres"

	orgsdomain "example.com/plateful/internal/modules/orgs/domain"
)

const updateOrgNameSQL = `
	UPDATE orgs AS o SET name = $2, updated_at = $3, version = version + 1
	WHERE o.id = $1 AND o.version = $4 AND o.deleted_at IS NULL
	RETURNING ` + orgColumns

// UpdateOrgName renames an organisation at version and increments the
// version, or returns ErrOrgVersionConflict. The name of an organisation
// that is a restaurant is the restaurant's, unique across the platform, so a
// rename can also return ErrOrgNameTaken.
func (s *Store) UpdateOrgName(ctx context.Context, id orgslib.ID, name string, version int64, now time.Time) (orgsdomain.Org, error) {
	rows, err := s.db.Query(ctx, updateOrgNameSQL, id, name, now, version)
	if err != nil {
		return orgsdomain.Org{}, constraintError(err)
	}
	o, err := pgx.CollectExactlyOneRow(rows, scanOrg)
	if postgres.IsNoRows(err) {
		return orgsdomain.Org{}, orgsdomain.ErrOrgVersionConflict
	}
	return o, constraintError(err)
}
