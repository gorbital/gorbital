package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/shelfie/internal/modules/books/domain"
)

const updateBookSQL = `
	UPDATE books SET title = $3, author = $4, isbn = $5, status = $6, updated_at = $7
	WHERE owner_id = $1 AND id = $2
	RETURNING ` + bookColumns

// UpdateBook saves b, or returns ErrBookNotFound or ErrISBNTaken.
func (s *Store) UpdateBook(ctx context.Context, b domain.Book) (domain.Book, error) {
	rows, err := s.db.Query(ctx, updateBookSQL, b.OwnerID, b.ID, b.Title, b.Author, nullable(b.ISBN), b.Status, b.UpdatedAt)
	if err == nil {
		b, err = pgx.CollectExactlyOneRow(rows, scanBook)
	}
	return b, driverError(err)
}
