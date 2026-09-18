package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/plateful/internal/modules/notifications/domain"
)

const insertEndpointSQL = `
	INSERT INTO notification_endpoints (id, org_id, created_by, label, url, last_delivery_at,
	                                    last_status, last_error, version, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	RETURNING ` + endpointColumns

// InsertEndpoint stores a new endpoint, or returns ErrEndpointLabelTaken
// when the organisation already has one with that label, ignoring case.
//
// RETURNING leaves the url out: the caller passed it in and already has it,
// so there is no reason for it to come back and sit in a second variable on
// the way to a 201 that doesn't include it.
func (s *Store) InsertEndpoint(ctx context.Context, e domain.Endpoint) (domain.Endpoint, error) {
	rows, err := s.db.Query(ctx, insertEndpointSQL,
		e.ID, e.OrgID, e.CreatedBy, e.Label, e.URL.Secret(), nullTime(e.LastDeliveryAt),
		e.LastStatus, e.LastError, e.Version, e.CreatedAt, e.UpdatedAt)
	if err != nil {
		return domain.Endpoint{}, constraintError(err)
	}
	saved, err := pgx.CollectExactlyOneRow(rows, scanEndpoint)
	if err != nil {
		return domain.Endpoint{}, constraintError(err)
	}
	saved.URL = e.URL
	return saved, nil
}
