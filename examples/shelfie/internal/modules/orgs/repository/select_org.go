package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	orgslib "gorbital.dev/modules/orgs"
	"gorbital.dev/modules/postgres"

	orgsdomain "example.com/shelfie/internal/modules/orgs/domain"
)

const (
	selectOrgSQL = `SELECT ` + orgColumns + ` FROM orgs o WHERE o.id = $1 AND o.deleted_at IS NULL`
	forUpdate    = ` FOR UPDATE`
)

// SelectOrg returns a live organisation, or orgs.ErrOrgNotFound. lock locks
// the row until the transaction ends.
func (s *Store) SelectOrg(ctx context.Context, id orgslib.ID, lock bool) (orgsdomain.Org, error) {
	sql := selectOrgSQL
	if lock {
		sql += forUpdate
	}
	rows, err := s.db.Query(ctx, sql, id)
	if err != nil {
		return orgsdomain.Org{}, err
	}
	o, err := pgx.CollectExactlyOneRow(rows, scanOrg)
	if postgres.IsNoRows(err) {
		return orgsdomain.Org{}, orgslib.ErrOrgNotFound
	}
	return o, err
}
