package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/couriers/domain"
)

// updateCourierSQL writes the three fields a courier changes about
// themselves. active_order_id is not among them on purpose: it belongs to
// the orders module, which sets it in its own transaction when it assigns
// and releases a courier, and this module would only ever fight it.
const updateCourierSQL = `
	UPDATE couriers
	SET display_name = $2, vehicle = $3, available = $4, updated_at = $5,
	    version = version + 1
	WHERE id = $1 AND version = $6
	RETURNING ` + courierColumns

// UpdateCourier saves c when the stored version is still c.Version and
// returns it with the next version. It returns ErrCourierVersionConflict
// when no row has that version (changed or deleted).
func (s *Store) UpdateCourier(ctx context.Context, c domain.Courier) (domain.Courier, error) {
	rows, err := s.db.Query(ctx, updateCourierSQL,
		c.ID, c.DisplayName, c.Vehicle, c.Available, c.UpdatedAt, c.Version)
	if err != nil {
		return domain.Courier{}, constraintError(err)
	}
	updated, err := pgx.CollectExactlyOneRow(rows, scanCourier)
	if postgres.IsNoRows(err) {
		return domain.Courier{}, domain.ErrCourierVersionConflict
	}
	return updated, constraintError(err)
}
