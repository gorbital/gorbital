package repository

import (
	"context"

	"example.com/plateful/internal/modules/orders/domain"
)

// docs:start take-stock

// TakeStock reduces a limited dish's stock by quantity, or returns
// ErrItemOutOfStock when there isn't that much left. A dish with unlimited
// stock (stock IS NULL) is left alone.
//
// menu_items is the menus module's table. The orders module writes it here,
// in the transaction that inserts the order, because the two have to be one
// change: an order that took the last portion and a menu that still offers
// it is the bug this prevents. The condition is in the UPDATE rather than in
// Go, so even without the row lock PlaceOrder takes, two orders racing for
// the last portion cannot both win.
func (s *Store) TakeStock(ctx context.Context, orgID, itemID string, quantity int) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE menu_items
		SET stock = stock - $3
		WHERE org_id = $1 AND id = $2 AND stock IS NOT NULL AND stock >= $3`,
		orgID, itemID, quantity)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Either the dish is unlimited, which is the ordinary case, or it
		// has run out since it was read.
		return s.unlimitedOrOutOfStock(ctx, orgID, itemID)
	}
	return nil
}

func (s *Store) unlimitedOrOutOfStock(ctx context.Context, orgID, itemID string) error {
	var unlimited bool
	err := s.db.QueryRow(ctx,
		`SELECT stock IS NULL FROM menu_items WHERE org_id = $1 AND id = $2`, orgID, itemID).Scan(&unlimited)
	switch {
	case err != nil:
		return err
	case unlimited:
		return nil
	}
	return domain.ErrItemOutOfStock
}

// docs:end take-stock

// SetCourierOrder writes the courier's current assignment. couriers is the
// couriers module's platform-scoped table, which has no org_id at all: this
// is the one write in the module that crosses out of a tenant, and it
// happens inside the transaction that changed the order, so a courier is
// never left holding an order that doesn't name them.
func (s *Store) SetCourierOrder(ctx context.Context, courierID, orderID string) error {
	tag, err := s.db.Exec(ctx, `
		UPDATE couriers SET active_order_id = $2, updated_at = now(), version = version + 1
		WHERE id = $1 AND ($2 = '' OR (available AND active_order_id IN ('', $2)))`,
		courierID, orderID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrCourierUnavailable
	}
	return nil
}
