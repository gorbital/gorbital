package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/orders/domain"
)

const insertOrderSQL = `
	INSERT INTO orders (id, org_id, customer_id, courier_id, status, address, note,
	                    total_minor, currency, scheduled_for, placed_at, accepted_at, ready_at,
	                    collected_at, delivered_at, closed_at, closed_reason, version, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
	RETURNING ` + orderColumns

// docs:start insert-order

// InsertOrder stores the order and its lines. Both statements run on the
// same transaction, so an order never exists without the dishes it is for.
//
// The lines are written with CopyFrom, one round trip for however many
// dishes the basket held, and they carry the name and price the order was
// placed at rather than pointing at the menu.
func (s *Store) InsertOrder(ctx context.Context, o domain.Order) (domain.Order, error) {
	rows, err := s.db.Query(ctx, insertOrderSQL,
		o.ID, o.OrgID, o.CustomerID, o.CourierID, o.Status, o.Address, o.Note,
		o.TotalMinor, o.Currency, nullTime(o.ScheduledFor), o.PlacedAt, nullTime(o.AcceptedAt),
		nullTime(o.ReadyAt), nullTime(o.CollectedAt), nullTime(o.DeliveredAt), nullTime(o.ClosedAt),
		o.ClosedReason, o.Version, o.CreatedAt, o.UpdatedAt)
	if err != nil {
		return domain.Order{}, constraintError(err)
	}
	stored, err := pgx.CollectExactlyOneRow(rows, scanOrder)
	if err != nil {
		return domain.Order{}, constraintError(err)
	}
	lines := make([][]any, len(o.Lines))
	for i, line := range o.Lines {
		lines[i] = []any{o.ID, i, line.ItemID, line.Name, line.PriceMinor, line.Quantity}
	}
	copier, ok := s.db.(interface {
		CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error)
	})
	if !ok {
		return domain.Order{}, errNoCopy
	}
	_, err = copier.CopyFrom(ctx, pgx.Identifier{"order_lines"},
		[]string{"order_id", "position", "item_id", "name", "price_minor", "quantity"},
		pgx.CopyFromRows(lines))
	if err != nil {
		return domain.Order{}, constraintError(err)
	}
	stored.Lines = o.Lines
	return stored, nil
}

// docs:end insert-order

// constraintError turns the constraint violations the use cases handle into
// domain errors. An order for an organisation that doesn't exist breaks the
// foreign key on org_id, which the database refuses even if a use case
// forgot to look.
func constraintError(err error) error {
	if constraint, ok := postgres.ForeignKeyViolation(err); ok {
		switch constraint {
		case "orders_org_id_fkey":
			return domain.ErrRestaurantNotFound
		}
	}
	return err
}
