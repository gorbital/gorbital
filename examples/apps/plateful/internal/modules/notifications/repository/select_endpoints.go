package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/plateful/internal/modules/notifications/domain"
)

const selectEndpointsSQL = `
	SELECT ` + endpointColumns + `, ` + hostExpr + ` FROM notification_endpoints
	WHERE org_id = $1
	ORDER BY created_at, id`

// SelectEndpoints returns every endpoint of orgID, oldest first, with the
// host each one points at but not its URL: PostgreSQL computes the host, so
// the secret never crosses the wire. Both callers — the list operation,
// which shows the host, and the fanout job, which wants only the IDs — are
// served by that, and neither can leak what it never received.
func (s *Store) SelectEndpoints(ctx context.Context, orgID string) ([]domain.Endpoint, error) {
	rows, err := s.db.Query(ctx, selectEndpointsSQL, orgID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanEndpointWithHost)
}
