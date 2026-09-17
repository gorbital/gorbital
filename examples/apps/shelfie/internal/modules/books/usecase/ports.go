package usecase

import (
	"context"

	"example.com/shelfie/internal/modules/books/domain"
)

// ListQuery selects books from one reader's shelf.
type ListQuery struct {
	OwnerID string
	// Status keeps books with this status; empty keeps all.
	Status domain.Status
	// Limit is how many books to return, newest first.
	Limit int
}

// docs:start store

// Store reads and writes books; repository.Store implements it with SQL.
// Every method is limited to one reader's books.
type Store interface {
	// InsertBook stores a new book, or returns ErrISBNTaken.
	InsertBook(ctx context.Context, b domain.Book) (domain.Book, error)
	// SelectBook returns one of ownerID's books, or ErrBookNotFound.
	SelectBook(ctx context.Context, ownerID, id string) (domain.Book, error)
	// SelectBooks returns up to q.Limit books, newest first.
	SelectBooks(ctx context.Context, q ListQuery) ([]domain.Book, error)
	// UpdateBook saves b, or returns ErrBookNotFound or ErrISBNTaken.
	UpdateBook(ctx context.Context, b domain.Book) (domain.Book, error)
	// DeleteBook removes one of ownerID's books, or returns ErrBookNotFound.
	DeleteBook(ctx context.Context, ownerID, id string) error
	// CountBooks returns how many books are on ownerID's shelf.
	CountBooks(ctx context.Context, ownerID string) (int, error)
	// InsertShelf stores a shelf; a name the owner already has is left as
	// it is.
	InsertShelf(ctx context.Context, s domain.Shelf) error
	// SelectShelves returns ownerID's shelves, oldest first.
	SelectShelves(ctx context.Context, ownerID string) ([]domain.Shelf, error)
}

// docs:end store
