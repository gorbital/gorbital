package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/payments/domain"
)

const updatePaymentSQL = `
	UPDATE order_payments
	SET status = $2, provider_ref = $3, failure_reason = $4, updated_at = $5,
	    version = version + 1
	WHERE id = $1 AND version = $6
	RETURNING ` + paymentColumns

// UpdatePayment saves p when the stored version is still p.Version and
// returns it with the next version. Nothing but the status, the reference
// and the reason ever changes: the money, the order and the customer are
// what the row is about.
func (s *Store) UpdatePayment(ctx context.Context, p domain.Payment) (domain.Payment, error) {
	rows, err := s.db.Query(ctx, updatePaymentSQL,
		p.ID, string(p.Status), p.ProviderRef, p.FailureReason, p.UpdatedAt, p.Version)
	if err != nil {
		return domain.Payment{}, constraintError(err)
	}
	updated, err := pgx.CollectExactlyOneRow(rows, scanPayment)
	if postgres.IsNoRows(err) {
		// The caller held the row with FOR UPDATE, so a missing version
		// means the payment itself is gone.
		return domain.Payment{}, domain.ErrPaymentNotFound
	}
	return updated, constraintError(err)
}
