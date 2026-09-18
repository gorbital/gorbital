package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/plateful/internal/modules/payments/domain"
)

const insertPaymentSQL = `
	INSERT INTO order_payments (` + paymentColumns + `)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	RETURNING ` + paymentColumns

// InsertPayment stores a new payment. The unique index on order_id decides
// a race between two pay calls for one order rather than a read the writer
// takes on trust, and the loser gets domain.ErrPaymentNotPayable.
func (s *Store) InsertPayment(ctx context.Context, p domain.Payment) (domain.Payment, error) {
	rows, err := s.db.Query(ctx, insertPaymentSQL,
		p.ID, p.OrgID, p.OrderID, p.CustomerID, p.AmountMinor, p.Currency, string(p.Status),
		p.ProviderRef, p.FailureReason, p.Version, p.CreatedAt, p.UpdatedAt)
	if err == nil {
		p, err = pgx.CollectExactlyOneRow(rows, scanPayment)
	}
	return p, constraintError(err)
}
