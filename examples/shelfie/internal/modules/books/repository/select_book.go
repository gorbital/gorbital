package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/shelfie/internal/modules/books/domain"
)

const selectBookSQL = `SELECT ` + bookColumns + ` FROM books WHERE owner_id = $1 AND id = $2`

// SelectBook returns one of ownerID's books, or ErrBookNotFound.
func (s *Store) SelectBook(ctx context.Context, ownerID, id string) (domain.Book, error) {
	rows, err := s.db.Query(ctx, selectBookSQL, ownerID, id)
	if err != nil {
		return domain.Book{}, err
	}
	b, err := pgx.CollectExactlyOneRow(rows, scanBook)
	return b, driverError(err)
}
