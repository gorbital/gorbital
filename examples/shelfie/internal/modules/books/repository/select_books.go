package repository

import (
	"context"

	"github.com/jackc/pgx/v5"

	"example.com/shelfie/internal/modules/books/domain"
	"example.com/shelfie/internal/modules/books/usecase"
)

const selectBooksSQL = `
	SELECT ` + bookColumns + ` FROM books
	WHERE owner_id = $1 AND ($2::text = '' OR status = $2)
	ORDER BY created_at DESC, id DESC
	LIMIT $3`

// SelectBooks returns up to q.Limit of an owner's books, newest first.
func (s *Store) SelectBooks(ctx context.Context, q usecase.ListQuery) ([]domain.Book, error) {
	rows, err := s.db.Query(ctx, selectBooksSQL, q.OwnerID, string(q.Status), q.Limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanBook)
}
