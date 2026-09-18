package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/orders/domain"
)

// errNoCopy is the programming error of running a bulk insert on something
// that isn't a pgx connection or transaction.
var errNoCopy = errors.New("orders: the store's connection can't copy rows")

const updateOrderSQL = `
	UPDATE orders
	SET status = $2, courier_id = $3, accepted_at = $4, ready_at = $5, collected_at = $6,
	    delivered_at = $7, closed_at = $8, closed_reason = $9, updated_at = $10,
	    version = version + 1
	WHERE id = $1
	RETURNING ` + orderColumns

// UpdateOrder saves what a status change touched: the status itself, the
// courier, the step's timestamp, and how it ended. The fields a customer
// chose — the dishes, the address, the total — are never written again.
func (s *Store) UpdateOrder(ctx context.Context, o domain.Order) (domain.Order, error) {
	rows, err := s.db.Query(ctx, updateOrderSQL,
		o.ID, o.Status, o.CourierID, nullTime(o.AcceptedAt), nullTime(o.ReadyAt),
		nullTime(o.CollectedAt), nullTime(o.DeliveredAt), nullTime(o.ClosedAt),
		o.ClosedReason, o.UpdatedAt)
	if err != nil {
		return domain.Order{}, err
	}
	updated, err := pgx.CollectExactlyOneRow(rows, scanOrder)
	if postgres.IsNoRows(err) {
		return domain.Order{}, domain.ErrOrderNotFound
	}
	updated.Lines = o.Lines
	return updated, err
}

// CountOpenOrders counts one restaurant's orders that haven't finished, for
// the orders.max_open_per_restaurant setting.
func (s *Store) CountOpenOrders(ctx context.Context, orgID string) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, `
		SELECT count(*) FROM orders
		WHERE org_id = $1 AND status IN ('placed', 'accepted', 'preparing', 'ready', 'collected')`,
		orgID).Scan(&n)
	return n, err
}
