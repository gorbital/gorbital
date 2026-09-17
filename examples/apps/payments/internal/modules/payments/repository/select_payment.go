package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/payments/internal/modules/payments/domain"
)

const selectPaymentSQL = `SELECT ` + paymentColumns + ` FROM payments WHERE id = $1`

// SelectPayment returns the payment with the ID, or
// domain.ErrPaymentNotFound.
func (s *Store) SelectPayment(ctx context.Context, id string) (domain.Payment, error) {
	rows, err := s.db.Query(ctx, selectPaymentSQL, id)
	if err != nil {
		return domain.Payment{}, err
	}
	payment, err := pgx.CollectExactlyOneRow(rows, scanPayment)
	if postgres.IsNoRows(err) {
		return domain.Payment{}, domain.ErrPaymentNotFound
	}
	return payment, err
}
