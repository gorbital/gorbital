package repository

import (
	"context"

	"example.com/shelfie/internal/modules/books/domain"
)

const deleteBookSQL = `DELETE FROM books WHERE owner_id = $1 AND id = $2`

// DeleteBook removes one of ownerID's books, or returns ErrBookNotFound.
func (s *Store) DeleteBook(ctx context.Context, ownerID, id string) error {
	tag, err := s.db.Exec(ctx, deleteBookSQL, ownerID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrBookNotFound
	}
	return nil
}
