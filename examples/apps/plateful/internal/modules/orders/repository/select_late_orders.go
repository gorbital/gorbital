package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"example.com/plateful/internal/modules/orders/domain"
)

// docs:start late-orders-sql

// selectLateOrdersSQL reads every restaurant's late orders in one query, the
// one kept longest first, and the sweep groups them by organisation in Go.
// The partial index orders_open matches its WHERE clause exactly, so it
// visits only orders a kitchen still has.
//
// It is the one statement of this module with no owner filter at all: the
// orders_late_sweep job runs for the whole platform, outside any request,
// and each restaurant is told only about its own orders because the report
// is split by org_id before anything is sent.
const selectLateOrdersSQL = `
	SELECT ` + orderColumns + ` FROM orders
	WHERE status IN ('accepted', 'preparing', 'ready')
	  AND accepted_at IS NOT NULL AND accepted_at < $1
	ORDER BY accepted_at, id
	LIMIT $2`

// SelectLateOrders returns up to limit orders accepted before the given time
// and not yet finished, across organisations.
func (s *Store) SelectLateOrders(ctx context.Context, acceptedBefore time.Time, limit int) ([]domain.Order, error) {
	rows, err := s.db.Query(ctx, selectLateOrdersSQL, acceptedBefore, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanOrder)
}

// docs:end late-orders-sql
