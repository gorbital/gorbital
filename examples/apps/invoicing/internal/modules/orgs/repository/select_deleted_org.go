package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	orgslib "gorbital.dev/modules/orgs"
	"gorbital.dev/modules/postgres"

	orgsdomain "example.com/invoicing/internal/modules/orgs/domain"
)

const selectDeletedOrgSQL = `SELECT ` + orgColumns + ` FROM orgs o WHERE o.id = $1 AND o.deleted_at IS NOT NULL FOR UPDATE`

// SelectDeletedOrg returns a deleted organisation that isn't purged yet, or
// orgs.ErrOrgNotFound, and locks it.
func (s *Store) SelectDeletedOrg(ctx context.Context, id orgslib.ID) (orgsdomain.Org, error) {
	rows, err := s.db.Query(ctx, selectDeletedOrgSQL, id)
	if err != nil {
		return orgsdomain.Org{}, err
	}
	o, err := pgx.CollectExactlyOneRow(rows, scanOrg)
	if postgres.IsNoRows(err) {
		return orgsdomain.Org{}, orgslib.ErrOrgNotFound
	}
	return o, err
}
