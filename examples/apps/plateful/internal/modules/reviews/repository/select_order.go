package repository

import (
	"context"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/reviews/domain"
)

// docs:start select-order

// selectOrderSQL reads the four things a review needs to know about an
// order.
//
// orders is the orders module's table, and this module reads it with SQL on
// purpose: a module never imports another module's layers, so the choice is
// between four column names and a Go dependency that the architecture test
// forbids. Four column names it is. They are part of the app's shared
// contract (see the table list in the build notes), so renaming one is a
// change to more than one module.
const selectOrderSQL = `SELECT org_id, customer_id, status FROM orders WHERE id = $1`

// SelectOrder returns what this module needs to know about an order, or
// ErrOrderNotFound.
func (s *Store) SelectOrder(ctx context.Context, orderID string) (domain.OrderFacts, error) {
	var o domain.OrderFacts
	err := s.db.QueryRow(ctx, selectOrderSQL, orderID).
		Scan(&o.OrgID, &o.CustomerID, &o.Status)
	if postgres.IsNoRows(err) {
		return domain.OrderFacts{}, domain.ErrOrderNotFound
	}
	return o, err
}

// docs:end select-order
