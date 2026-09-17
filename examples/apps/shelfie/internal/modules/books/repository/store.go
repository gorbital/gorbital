// Package repository stores books in PostgreSQL with hand-written SQL, one
// file per operation. The table comes from db/migrations.
package repository

import (
	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/shelfie/internal/modules/books/domain"
	"example.com/shelfie/internal/modules/books/usecase"
)

// Store implements usecase.Store on a pool or a transaction.
type Store struct {
	db postgres.DBTX
}

var _ usecase.Store = (*Store)(nil)

// NewStore returns a store on db.
func NewStore(db postgres.DBTX) *Store { return &Store{db: db} }

// bookColumns are the columns scanBook reads, in its order.
const bookColumns = `id, owner_id, title, author, coalesce(isbn, ''), status, created_at, updated_at`

func scanBook(row pgx.CollectableRow) (domain.Book, error) {
	var b domain.Book
	err := row.Scan(&b.ID, &b.OwnerID, &b.Title, &b.Author, &b.ISBN, &b.Status, &b.CreatedAt, &b.UpdatedAt)
	b.CreatedAt, b.UpdatedAt = b.CreatedAt.UTC(), b.UpdatedAt.UTC()
	return b, err
}

// nullable stores an empty string as NULL.
func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// driverError turns what the use cases handle into domain errors: no row,
// and the unique ISBN per shelf.
func driverError(err error) error {
	switch {
	case err == nil:
		return nil
	case postgres.IsNoRows(err):
		return domain.ErrBookNotFound
	}
	if constraint, ok := postgres.UniqueViolation(err); ok && constraint == "books_owner_isbn" {
		return domain.ErrISBNTaken
	}
	return err
}
