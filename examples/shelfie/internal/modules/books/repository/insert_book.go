package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/shelfie/internal/modules/books/domain"
)

// docs:start insert-book

const insertBookSQL = `
	INSERT INTO books (id, owner_id, title, author, isbn, status, created_at, updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	RETURNING ` + bookColumns

// InsertBook stores a new book, or returns ErrISBNTaken.
func (s *Store) InsertBook(ctx context.Context, b domain.Book) (domain.Book, error) {
	rows, err := s.db.Query(ctx, insertBookSQL,
		b.ID, b.OwnerID, b.Title, b.Author, nullable(b.ISBN), b.Status, b.CreatedAt, b.UpdatedAt)
	if err == nil {
		b, err = pgx.CollectExactlyOneRow(rows, scanBook)
	}
	return b, driverError(err)
}

// docs:end insert-book
