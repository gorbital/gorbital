package repository

import (
	"context"

	"example.com/plateful/internal/modules/notifications/domain"
)

const updateDeliverySQL = `
	UPDATE notification_endpoints
	SET last_delivery_at = $3, last_status = $4, last_error = $5,
	    version = version + 1, updated_at = $6
	WHERE org_id = $1 AND id = $2`

// UpdateDelivery records the outcome of e's last delivery attempt.
//
// There is no version check: this is the job writing down what happened, not
// a user changing the endpoint, and two attempts racing should both be
// allowed to land — the loser's row is simply overwritten by the later
// outcome, which is the one the restaurant wants to see. A missing row is
// not an error either: the endpoint may have been removed while the delivery
// was in flight, and that is the restaurant revoking a webhook, not a
// failure to report.
func (s *Store) UpdateDelivery(ctx context.Context, e domain.Endpoint) error {
	_, err := s.db.Exec(ctx, updateDeliverySQL,
		e.OrgID, e.ID, nullTime(e.LastDeliveryAt), e.LastStatus, e.LastError, e.UpdatedAt)
	return err
}
