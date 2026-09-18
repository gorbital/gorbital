package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/orders/domain"
)

const updateOrderSQL = `
	UPDATE orders
	SET status = $3, address = $4, note = $5, updated_at = $6, version = version + 1
	WHERE id = $1 AND org_id = $2 AND version = $7
	RETURNING ` + orderColumns

// UpdateOrder saves order when the stored version is still order.Version and
// returns it with the next version. It returns ErrOrderVersionConflict when
// no row has that version (changed, deleted or not the organisation's).
func (s *Store) UpdateOrder(ctx context.Context, order domain.Order) (domain.Order, error) {
	rows, err := s.db.Query(ctx, updateOrderSQL,
		order.ID, order.OrgID, order.Status, order.Address, order.Note, order.UpdatedAt, order.Version)
	if err != nil {
		return domain.Order{}, constraintError(err)
	}
	updated, err := pgx.CollectExactlyOneRow(rows, scanOrder)
	if postgres.IsNoRows(err) {
		return domain.Order{}, domain.ErrOrderVersionConflict
	}
	return updated, constraintError(err)
}
