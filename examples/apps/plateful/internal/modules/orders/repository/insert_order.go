package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/plateful/internal/modules/orders/domain"
)

const insertOrderSQL = `
	INSERT INTO orders (id, org_id, created_by, status, address, note, version, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	RETURNING ` + orderColumns

// InsertOrder stores a new order.
func (s *Store) InsertOrder(ctx context.Context, order domain.Order) (domain.Order, error) {
	rows, err := s.db.Query(ctx, insertOrderSQL,
		order.ID, order.OrgID, order.CreatedBy, order.Status, order.Address, order.Note, order.Version, order.CreatedAt, order.UpdatedAt)
	if err == nil {
		order, err = pgx.CollectExactlyOneRow(rows, scanOrder)
	}
	return order, constraintError(err)
}
