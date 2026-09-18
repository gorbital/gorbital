// Package repository stores the orders module's orders in PostgreSQL with
// hand-written SQL, one file per operation. The tables come from
// db/migrations.
//
// A few of its statements read tables other modules own — restaurants,
// menu_items, couriers, order_payments. That is deliberate and is the only
// way a rule can span modules inside one transaction: a module never imports
// another module's layers (internal/modules/architecture_test.go), and a
// call to another module's use case could not join this transaction. Each
// such statement says which module owns the table it touches.
package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/orders/domain"
	"example.com/plateful/internal/modules/orders/usecase"
)

// Store implements usecase.Store. It runs on the pool, or on a transaction
// inside TxManager.InTx.
type Store struct {
	db postgres.DBTX
}

var _ usecase.Store = (*Store)(nil)

// NewStore returns a store on pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{db: pool} }

// newStoreOn returns a store running every statement on db, the transaction
// TxManager.InTx opened.
func newStoreOn(db postgres.DBTX) *Store { return &Store{db: db} }

// orderColumns are the columns scanOrder reads, in its order.
const orderColumns = `id, org_id, restaurant_id, customer_id, courier_id, status, address, note, ` +
	`total_minor, currency, scheduled_for, placed_at, accepted_at, ready_at, collected_at, ` +
	`delivered_at, closed_at, closed_reason, version, created_at, updated_at`

func scanOrder(row pgx.CollectableRow) (domain.Order, error) {
	var o domain.Order
	var scheduled, accepted, ready, collected, delivered, closed *time.Time
	err := row.Scan(&o.ID, &o.OrgID, &o.RestaurantID, &o.CustomerID, &o.CourierID, &o.Status,
		&o.Address, &o.Note, &o.TotalMinor, &o.Currency, &scheduled, &o.PlacedAt,
		&accepted, &ready, &collected, &delivered, &closed, &o.ClosedReason,
		&o.Version, &o.CreatedAt, &o.UpdatedAt)
	for target, value := range map[*time.Time]*time.Time{
		&o.ScheduledFor: scheduled, &o.AcceptedAt: accepted, &o.ReadyAt: ready,
		&o.CollectedAt: collected, &o.DeliveredAt: delivered, &o.ClosedAt: closed,
	} {
		if value != nil {
			*target = value.UTC()
		}
	}
	o.PlacedAt, o.CreatedAt, o.UpdatedAt = o.PlacedAt.UTC(), o.CreatedAt.UTC(), o.UpdatedAt.UTC()
	return o, err
}

// nullTime returns t as a parameter, or NULL when it is zero: a step the
// order hasn't taken is absent, not the year 1.
func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}

// SelectRestaurant returns the organisation and whether the restaurant is
// taking orders. restaurants is the restaurants module's table; a whole
// order points at it through the migration's foreign key, so this module
// reads it here rather than importing that module's layers. "Taking orders"
// is the same rule the restaurants module's domain applies: open, and inside
// its hours, with a kitchen that works past midnight counted correctly.
func (s *Store) SelectRestaurant(ctx context.Context, restaurantID string) (string, bool, error) {
	const sql = `
	SELECT org_id,
	       status = 'open' AND (
	           opens_minute = closes_minute
	           OR (opens_minute < closes_minute AND m >= opens_minute AND m < closes_minute)
	           OR (opens_minute > closes_minute AND (m >= opens_minute OR m < closes_minute))
	       )
	FROM restaurants, LATERAL (SELECT (EXTRACT(HOUR FROM now() AT TIME ZONE 'UTC') * 60
	                                 + EXTRACT(MINUTE FROM now() AT TIME ZONE 'UTC'))::int AS m) AS clock
	WHERE id = $1`
	var orgID string
	var accepting bool
	err := s.db.QueryRow(ctx, sql, restaurantID).Scan(&orgID, &accepting)
	if postgres.IsNoRows(err) {
		return "", false, domain.ErrRestaurantNotFound
	}
	return orgID, accepting, err
}

// CourierOfUser returns the courier profile of a user account, or
// ErrNotACourier. couriers is the couriers module's platform-scoped table:
// it has no org_id, because a courier delivers for many restaurants.
func (s *Store) CourierOfUser(ctx context.Context, userID string) (string, error) {
	var id string
	err := s.db.QueryRow(ctx, `SELECT id FROM couriers WHERE user_id = $1`, userID).Scan(&id)
	if postgres.IsNoRows(err) {
		return "", domain.ErrNotACourier
	}
	return id, err
}

// SelectAvailableCouriers returns couriers who are free right now, from the
// couriers module's table.
func (s *Store) SelectAvailableCouriers(ctx context.Context, limit int) ([]usecase.AvailableCourier, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, display_name FROM couriers
		WHERE available AND active_order_id = ''
		ORDER BY id
		LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (usecase.AvailableCourier, error) {
		var c usecase.AvailableCourier
		return c, row.Scan(&c.ID, &c.DisplayName)
	})
}

// docs:start payment-status-sql

// PaymentStatus returns the status of an order's payment, or "" when the
// order has none yet. order_payments is the payments module's table; the
// rule that a restaurant may not accept an unpaid order is enforced in this
// module's AcceptOrder, and this is the half of it that reads the other
// module's state — in SQL, inside the same transaction, because nothing in
// gorbital lets one module's use case join another's.
func (s *Store) PaymentStatus(ctx context.Context, orgID, orderID string) (string, error) {
	var status string
	err := s.db.QueryRow(ctx,
		`SELECT status FROM order_payments WHERE org_id = $1 AND order_id = $2`, orgID, orderID).Scan(&status)
	if postgres.IsNoRows(err) {
		return "", nil
	}
	return status, err
}

// docs:end payment-status-sql

// SelectMenuItems returns the menu items of orgID with these IDs, from the
// menus module's table. lock locks the rows until the transaction ends, so
// two orders can't both take the last portion of the same dish.
func (s *Store) SelectMenuItems(ctx context.Context, orgID string, ids []string, lock bool) (map[string]usecase.MenuItem, error) {
	sql := `SELECT id, name, price_minor, currency, available, stock FROM menu_items WHERE org_id = $1 AND id = ANY($2)`
	if lock {
		sql += ` ORDER BY id FOR UPDATE`
	}
	rows, err := s.db.Query(ctx, sql, orgID, ids)
	if err != nil {
		return nil, err
	}
	items, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (usecase.MenuItem, error) {
		var m usecase.MenuItem
		return m, row.Scan(&m.ID, &m.Name, &m.PriceMin, &m.Currency, &m.Available, &m.Stock)
	})
	if err != nil {
		return nil, err
	}
	byID := make(map[string]usecase.MenuItem, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	return byID, nil
}
