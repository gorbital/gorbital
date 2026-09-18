package repository

import (
	"context"

	"example.com/plateful/internal/modules/notifications/domain"
)

const deleteEndpointSQL = `DELETE FROM notification_endpoints WHERE org_id = $1 AND id = $2`

// DeleteEndpoint removes one of orgID's endpoints, or returns
// ErrEndpointNotFound.
func (s *Store) DeleteEndpoint(ctx context.Context, orgID, id string) error {
	tag, err := s.db.Exec(ctx, deleteEndpointSQL, orgID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrEndpointNotFound
	}
	return nil
}
