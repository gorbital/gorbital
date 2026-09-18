package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/payments/domain"
	"example.com/plateful/internal/modules/payments/usecase"
)

// selectOrderSQL reads the orders table, which belongs to the orders
// module. That is deliberate and it is the only way the two modules can
// meet: internal/modules/architecture_test.go forbids a Go import of
// another module's layers, and the database is what they do share. Five
// columns, no join, no write — this module never changes an order.
//
// The column names are a contract with a module this one can't see, so they
// are listed here in full rather than with a star, and any change to them
// has to be made on both sides at once.
const selectOrderSQL = `SELECT org_id, customer_id, status, total_minor, currency FROM orders WHERE id = $1`

// SelectOrder returns the order's payment-relevant columns, or
// domain.ErrOrderNotFound.
func (s *Store) SelectOrder(ctx context.Context, orderID string) (usecase.Order, error) {
	rows, err := s.db.Query(ctx, selectOrderSQL, orderID)
	if err != nil {
		return usecase.Order{}, err
	}
	order, err := pgx.CollectExactlyOneRow(rows, func(row pgx.CollectableRow) (usecase.Order, error) {
		var o usecase.Order
		return o, row.Scan(&o.OrgID, &o.CustomerID, &o.Status, &o.TotalMinor, &o.Currency)
	})
	if postgres.IsNoRows(err) {
		return usecase.Order{}, domain.ErrOrderNotFound
	}
	return order, err
}
