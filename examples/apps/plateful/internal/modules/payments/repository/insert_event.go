package repository

import (
	"context"

	"example.com/plateful/internal/modules/payments/domain"
)

// insertEventSQL records a delivery unless its event ID is recorded
// already. ON CONFLICT DO NOTHING rather than a read first: the event ID is
// the primary key, so the database decides the race between two copies of
// one delivery, and the rows affected say which of them won.
const insertEventSQL = `
	INSERT INTO payment_events (event_id, payment_id, kind, received_at)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT (event_id) DO NOTHING`

// InsertEvent records e and reports whether this call was the one that
// recorded it. False means the provider has delivered this event before,
// and the caller must leave the payment alone.
func (s *Store) InsertEvent(ctx context.Context, e domain.Event) (bool, error) {
	tag, err := s.db.Exec(ctx, insertEventSQL, e.ID, e.PaymentID, e.Kind, e.ReceivedAt)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}
