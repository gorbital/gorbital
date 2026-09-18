package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/menus/domain"
)

const updateItemSQL = `
	UPDATE menu_items
	SET section = $3, position = $4, name = $5, description = $6, price_minor = $7,
	    currency = $8, available = $9, stock = $10, dietary = $11, photo_image_id = $12,
	    updated_at = $13, version = version + 1
	WHERE org_id = $1 AND id = $2 AND version = $14
	RETURNING ` + itemColumns

// UpdateItem saves item when the stored version is still item.Version and
// returns it with the next version. It returns ErrItemVersionConflict when
// no row has that version (changed or deleted), and ErrItemNameTaken.
func (s *Store) UpdateItem(ctx context.Context, item domain.Item) (domain.Item, error) {
	rows, err := s.db.Query(ctx, updateItemSQL,
		item.OrgID, item.ID, item.Section, item.Position, item.Name, item.Description,
		item.PriceMinor, item.Currency, item.Available, item.Stock, item.Dietary,
		item.PhotoImageID, item.UpdatedAt, item.Version)
	if err != nil {
		return domain.Item{}, constraintError(err)
	}
	updated, err := pgx.CollectExactlyOneRow(rows, scanItem)
	if postgres.IsNoRows(err) {
		return domain.Item{}, domain.ErrItemVersionConflict
	}
	return updated, constraintError(err)
}
