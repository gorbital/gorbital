package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/shelfie/internal/modules/partners/domain"
)

const selectPurchasesSQL = `
	SELECT ` + purchaseColumns + `
	FROM partner_purchases
	WHERE user_id = $1
	ORDER BY purchased_at DESC, id DESC
	LIMIT $2`

// SelectPurchases returns a reader's purchases, newest first.
func (s *Store) SelectPurchases(ctx context.Context, userID string, limit int) ([]domain.Purchase, error) {
	rows, err := s.db.Query(ctx, selectPurchasesSQL, userID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanPurchase)
}
