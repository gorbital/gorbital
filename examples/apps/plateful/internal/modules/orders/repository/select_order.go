package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/orders/domain"
)

const (
	selectOrderSQL = `SELECT ` + orderColumns + ` FROM orders WHERE id = $1 AND org_id = $2`
	forUpdate      = ` FOR UPDATE`
)

// SelectOrder returns one of orgID's orders, or ErrOrderNotFound.
// lock locks the row until the transaction ends.
func (s *Store) SelectOrder(ctx context.Context, orgID, id string, lock bool) (domain.Order, error) {
	sql := selectOrderSQL
	if lock {
		sql += forUpdate
	}
	rows, err := s.db.Query(ctx, sql, id, orgID)
	if err != nil {
		return domain.Order{}, err
	}
	order, err := pgx.CollectExactlyOneRow(rows, scanOrder)
	if postgres.IsNoRows(err) {
		return domain.Order{}, domain.ErrOrderNotFound
	}
	return order, err
}
