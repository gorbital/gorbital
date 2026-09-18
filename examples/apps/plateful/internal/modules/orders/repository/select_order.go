package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/orders/domain"
)

const (
	selectOrderSQL    = `SELECT ` + orderColumns + ` FROM orders WHERE id = $1`
	selectOrgOrderSQL = `SELECT ` + orderColumns + ` FROM orders WHERE org_id = $1 AND id = $2`
	forUpdate         = ` FOR UPDATE`
)

// docs:start select-order

// SelectOrder returns one order by ID, whichever organisation it belongs to.
// It is the query a customer's and a courier's routes use, because neither
// caller has an organisation to scope it by; the use case then compares the
// row against the caller and answers ErrOrderNotFound when it isn't theirs.
//
// A restaurant's own routes use SelectOrgOrder instead, where the
// organisation is part of the WHERE clause and there is nothing left to
// check. Prefer that shape wherever the caller has a tenant: a filter in the
// query can't be forgotten, and a comparison afterwards can.
func (s *Store) SelectOrder(ctx context.Context, id string, lock bool) (domain.Order, error) {
	return s.selectOne(ctx, selectOrderSQL, lock, id)
}

// docs:end select-order

// SelectOrgOrder returns one of the organisation's orders, or
// ErrOrderNotFound.
func (s *Store) SelectOrgOrder(ctx context.Context, orgID, id string, lock bool) (domain.Order, error) {
	return s.selectOne(ctx, selectOrgOrderSQL, lock, orgID, id)
}

func (s *Store) selectOne(ctx context.Context, sql string, lock bool, args ...any) (domain.Order, error) {
	if lock {
		sql += forUpdate
	}
	rows, err := s.db.Query(ctx, sql, args...)
	if err != nil {
		return domain.Order{}, err
	}
	o, err := pgx.CollectExactlyOneRow(rows, scanOrder)
	if postgres.IsNoRows(err) {
		return domain.Order{}, domain.ErrOrderNotFound
	}
	return o, err
}

// SelectLines returns the lines of the given orders, keyed by order ID, in
// the order they were sent.
func (s *Store) SelectLines(ctx context.Context, orderIDs []string) (map[string][]domain.Line, error) {
	if len(orderIDs) == 0 {
		return map[string][]domain.Line{}, nil
	}
	rows, err := s.db.Query(ctx, `
		SELECT order_id, item_id, name, price_minor, quantity
		FROM order_lines WHERE order_id = ANY($1)
		ORDER BY order_id, position`, orderIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lines := make(map[string][]domain.Line, len(orderIDs))
	for rows.Next() {
		var orderID string
		var l domain.Line
		if err := rows.Scan(&orderID, &l.ItemID, &l.Name, &l.PriceMinor, &l.Quantity); err != nil {
			return nil, err
		}
		lines[orderID] = append(lines[orderID], l)
	}
	return lines, rows.Err()
}
