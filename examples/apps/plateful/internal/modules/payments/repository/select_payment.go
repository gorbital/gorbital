package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/payments/domain"
)

const (
	selectPaymentSQL        = `SELECT ` + paymentColumns + ` FROM order_payments WHERE id = $1`
	selectPaymentByOrderSQL = `SELECT ` + paymentColumns + ` FROM order_payments WHERE order_id = $1`
)

// SelectPayment returns one payment by ID, or domain.ErrPaymentNotFound.
// lock locks the row until the transaction ends.
func (s *Store) SelectPayment(ctx context.Context, id string, lock bool) (domain.Payment, error) {
	return s.selectOne(ctx, selectPaymentSQL, id, lock)
}

// SelectPaymentByOrder returns the order's payment, or
// domain.ErrPaymentNotFound. There is at most one, which the migration's
// UNIQUE on order_id enforces.
func (s *Store) SelectPaymentByOrder(ctx context.Context, orderID string, lock bool) (domain.Payment, error) {
	return s.selectOne(ctx, selectPaymentByOrderSQL, orderID, lock)
}

func (s *Store) selectOne(ctx context.Context, sql, key string, lock bool) (domain.Payment, error) {
	if lock {
		sql += forUpdate
	}
	rows, err := s.db.Query(ctx, sql, key)
	if err != nil {
		return domain.Payment{}, err
	}
	p, err := pgx.CollectExactlyOneRow(rows, scanPayment)
	if postgres.IsNoRows(err) {
		return domain.Payment{}, domain.ErrPaymentNotFound
	}
	return p, err
}
