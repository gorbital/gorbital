package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/plateful/internal/modules/couriers/domain"
)

const insertCourierSQL = `
	INSERT INTO couriers (id, user_id, display_name, vehicle, available, active_order_id,
	                      version, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	RETURNING ` + courierColumns

// InsertCourier stores a new courier profile, or returns
// ErrCourierAlreadyRegistered when the account already has one.
func (s *Store) InsertCourier(ctx context.Context, c domain.Courier) (domain.Courier, error) {
	rows, err := s.db.Query(ctx, insertCourierSQL,
		c.ID, c.UserID, c.DisplayName, c.Vehicle, c.Available, c.ActiveOrderID,
		c.Version, c.CreatedAt, c.UpdatedAt)
	if err == nil {
		c, err = pgx.CollectExactlyOneRow(rows, scanCourier)
	}
	return c, constraintError(err)
}
