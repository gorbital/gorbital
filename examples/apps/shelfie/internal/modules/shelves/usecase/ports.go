package usecase

import (
	"context"
	"time"

	"gorbital.dev/page"

	"example.com/shelfie/internal/modules/shelves/domain"
)

// ListQuery selects one page of an owner's shelves.
type ListQuery struct {
	OwnerID string
	// Visibility keeps shelves with this visibility; empty keeps all.
	Visibility domain.Visibility
	// Sort is one of the sortable fields: created_at, updated_at or name.
	Sort page.SortField
	// After, when set, starts the page after this position.
	After *Position
	Limit int
}

// Position is where a page ended: the last shelf's sort value and ID.
type Position struct {
	Time time.Time // when sorting by created_at or updated_at
	Text string    // when sorting by a text field
	ID   string
}

// Store reads and writes shelves; repository.Store implements it with SQL.
// Every method is limited to one owner's shelves.
type Store interface {
	// InsertShelf stores a new shelf, or returns ErrShelfNameTaken.
	InsertShelf(ctx context.Context, shelf domain.Shelf) (domain.Shelf, error)
	// SelectShelf returns one of ownerID's shelves, or ErrShelfNotFound.
	// lock locks the row until the transaction ends.
	SelectShelf(ctx context.Context, ownerID, id string, lock bool) (domain.Shelf, error)
	// SelectShelves returns up to q.Limit shelves in q.Sort order, with the
	// ID breaking ties.
	SelectShelves(ctx context.Context, q ListQuery) ([]domain.Shelf, error)
	// UpdateShelf saves shelf when the stored version is still shelf.Version and
	// increments the version. It returns ErrShelfVersionConflict when the
	// version changed or the shelf is gone, and ErrShelfNameTaken.
	UpdateShelf(ctx context.Context, shelf domain.Shelf) (domain.Shelf, error)
	// DeleteShelf removes one of ownerID's shelves, or returns
	// ErrShelfNotFound.
	DeleteShelf(ctx context.Context, ownerID, id string) error
	// InTx runs fn in one transaction: it commits when fn returns nil and
	// rolls back otherwise.
	InTx(ctx context.Context, fn func(tx Store) error) error
}
