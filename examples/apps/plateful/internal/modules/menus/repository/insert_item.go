package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/plateful/internal/modules/menus/domain"
)

const insertItemSQL = `
	INSERT INTO menu_items (id, org_id, created_by, section, position, name, description,
	                        price_minor, currency, available, stock, dietary, photo_image_id,
	                        version, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	RETURNING ` + itemColumns

// InsertItem stores a new menu item, or returns ErrItemNameTaken when
// another of the organisation's items already uses the name, ignoring case.
func (s *Store) InsertItem(ctx context.Context, item domain.Item) (domain.Item, error) {
	rows, err := s.db.Query(ctx, insertItemSQL,
		item.ID, item.OrgID, item.CreatedBy, item.Section, item.Position, item.Name, item.Description,
		item.PriceMinor, item.Currency, item.Available, item.Stock, item.Dietary, item.PhotoImageID,
		item.Version, item.CreatedAt, item.UpdatedAt)
	if err == nil {
		item, err = pgx.CollectExactlyOneRow(rows, scanItem)
	}
	return item, constraintError(err)
}
