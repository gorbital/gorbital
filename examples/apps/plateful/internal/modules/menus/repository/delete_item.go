package repository

import (
	"context"

	"example.com/plateful/internal/modules/menus/domain"
)

const deleteItemSQL = `DELETE FROM menu_items WHERE org_id = $1 AND id = $2`

// DeleteItem removes one of the organisation's items, or returns
// ErrItemNotFound.
func (s *Store) DeleteItem(ctx context.Context, orgID, id string) error {
	tag, err := s.db.Exec(ctx, deleteItemSQL, orgID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrItemNotFound
	}
	return nil
}
