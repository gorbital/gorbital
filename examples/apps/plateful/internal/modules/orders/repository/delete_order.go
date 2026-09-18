package repository

import (
	"context"

	"example.com/plateful/internal/modules/orders/domain"
)

const deleteOrderSQL = `DELETE FROM orders WHERE id = $1 AND org_id = $2`

// DeleteOrder removes one of orgID's orders, or returns
// ErrOrderNotFound.
func (s *Store) DeleteOrder(ctx context.Context, orgID, id string) error {
	tag, err := s.db.Exec(ctx, deleteOrderSQL, id, orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrOrderNotFound
	}
	return nil
}
