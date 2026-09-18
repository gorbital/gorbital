package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/plateful/internal/modules/couriers/domain"
	"example.com/plateful/internal/modules/couriers/usecase"
)

// selectAvailableCouriersSQL reads the couriers a restaurant may dispatch.
//
// The WHERE clause matches the couriers_available partial index in the
// migration: on duty, and not already carrying something. No organisation
// appears in it, because none appears on the table; the caller's restaurant
// has been checked by the route's guard and has no bearing on which rows
// exist.
const selectAvailableCouriersSQL = `
	SELECT ` + courierColumns + `
	FROM couriers
	WHERE available AND active_order_id = ''
	ORDER BY id
	LIMIT $1`

// SelectAvailableCouriers returns up to q.Limit couriers who are available
// and free. It reads the whole row; the use case's caller sees only the
// fields the delivery layer puts in the response.
func (s *Store) SelectAvailableCouriers(ctx context.Context, q usecase.AvailableQuery) ([]domain.Courier, error) {
	rows, err := s.db.Query(ctx, selectAvailableCouriersSQL, q.Limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanCourier)
}
