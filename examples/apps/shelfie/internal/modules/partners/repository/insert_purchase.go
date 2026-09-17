package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"example.com/shelfie/internal/modules/partners/domain"
)

// docs:start insert-purchase

const insertPurchaseSQL = `
	INSERT INTO partner_purchases (id, partner, event_id, user_id, isbn, title, purchased_at, created_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	ON CONFLICT (partner, event_id) DO NOTHING
	RETURNING ` + purchaseColumns

const selectByEventSQL = `SELECT ` + purchaseColumns + ` FROM partner_purchases WHERE partner = $1 AND event_id = $2`

// InsertPurchase stores p, or, when the partner already sent this event ID,
// returns the purchase stored then and false. One statement decides: the
// unique constraint is the record of what has already been applied, so two
// deliveries arriving at once can't both win.
func (s *Store) InsertPurchase(ctx context.Context, p domain.Purchase) (domain.Purchase, bool, error) {
	rows, err := s.db.Query(ctx, insertPurchaseSQL,
		p.ID, p.Partner, p.EventID, p.UserID, p.ISBN, p.Title, p.PurchasedAt, p.CreatedAt)
	if err != nil {
		return domain.Purchase{}, false, err
	}
	stored, err := pgx.CollectExactlyOneRow(rows, scanPurchase)
	if err == nil {
		return stored, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.Purchase{}, false, err
	}
	// The row was already there: return what the first delivery stored.
	rows, err = s.db.Query(ctx, selectByEventSQL, p.Partner, p.EventID)
	if err != nil {
		return domain.Purchase{}, false, err
	}
	stored, err = pgx.CollectExactlyOneRow(rows, scanPurchase)
	return stored, false, err
}

// docs:end insert-purchase
