package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	orgsdomain "example.com/plateful/internal/modules/orgs/domain"
)

const selectPersonalOrgSQL = `SELECT ` + orgColumns + ` FROM orgs o WHERE o.created_by = $1 AND o.personal`

// SelectPersonalOrg returns userID's personal workspace, deleted or not, and
// whether there is one.
func (s *Store) SelectPersonalOrg(ctx context.Context, userID string) (orgsdomain.Org, bool, error) {
	rows, err := s.db.Query(ctx, selectPersonalOrgSQL, userID)
	if err != nil {
		return orgsdomain.Org{}, false, err
	}
	o, err := pgx.CollectExactlyOneRow(rows, scanOrg)
	if postgres.IsNoRows(err) {
		return orgsdomain.Org{}, false, nil
	}
	return o, err == nil, err
}
