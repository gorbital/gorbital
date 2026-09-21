package repository

import (
	"context"

	"example.com/shelfie/internal/modules/shelves/domain"
)

const deleteShelfSQL = `DELETE FROM shelves WHERE id = $1 AND owner_id = $2`

// DeleteShelf removes one of ownerID's shelves, or returns
// ErrShelfNotFound.
func (s *Store) DeleteShelf(ctx context.Context, ownerID, id string) error {
	tag, err := s.db.Exec(ctx, deleteShelfSQL, id, ownerID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrShelfNotFound
	}
	return nil
}
