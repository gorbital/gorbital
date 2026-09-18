package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/notifications/domain"
)

// selectEndpointSQL is the only query in the app that reads the url column.
//
// It is scoped by org_id as well as id even though ids are unguessable,
// because the job arguments name both and a worker that trusted the id alone
// would post one organisation's orders into another organisation's channel
// the first time an argument was built wrong.
//
// A deployment that turns row-level security on (ADR-0061,
// db/row_level_security.sql) has to think about this query: a worker runs
// outside a request, so no organisation is set on its connection and the
// policy would hide every row. postgres.WithoutRowLevelSecurity is how a
// system path says it means to read across organisations; this app does not
// apply the policies, so nothing calls it yet.
const selectEndpointSQL = `
	SELECT ` + endpointColumns + `, url FROM notification_endpoints
	WHERE org_id = $1 AND id = $2`

// SelectEndpointWithURL returns one of orgID's endpoints including its URL,
// or ErrEndpointNotFound. The delivery worker is its only caller.
func (s *Store) SelectEndpointWithURL(ctx context.Context, orgID, id string) (domain.Endpoint, error) {
	rows, err := s.db.Query(ctx, selectEndpointSQL, orgID, id)
	if err != nil {
		return domain.Endpoint{}, err
	}
	e, err := pgx.CollectExactlyOneRow(rows, scanEndpointWithURL)
	if postgres.IsNoRows(err) {
		return domain.Endpoint{}, domain.ErrEndpointNotFound
	}
	return e, err
}
