package usecase

import (
	"context"
	"time"

	"gorbital.dev/page"

	"example.com/shelfie/internal/modules/clubbooks/domain"
)

// ListQuery selects one page of an organisation's club books.
type ListQuery struct {
	OrgID string
	// Status keeps club books with this status; empty keeps all.
	Status domain.Status
	// Sort is one of the sortable fields: created_at, updated_at, title or author.
	Sort page.SortField
	// After, when set, starts the page after this position.
	After *Position
	Limit int
}

// Position is where a page ended: the last club book's sort value and ID.
type Position struct {
	Time time.Time // when sorting by created_at or updated_at
	Text string    // when sorting by a text field
	ID   string
}

// Store reads and writes club books; repository.Store implements it with SQL.
// Every method is limited to one organisation's club books.
type Store interface {
	// InsertClubBook stores a new club book, or returns ErrClubBookTitleTaken.
	InsertClubBook(ctx context.Context, clubBook domain.ClubBook) (domain.ClubBook, error)
	// SelectClubBook returns one of orgID's club books, or ErrClubBookNotFound.
	// lock locks the row until the transaction ends.
	SelectClubBook(ctx context.Context, orgID, id string, lock bool) (domain.ClubBook, error)
	// SelectClubBooks returns up to q.Limit club books in q.Sort order, with the
	// ID breaking ties.
	SelectClubBooks(ctx context.Context, q ListQuery) ([]domain.ClubBook, error)
	// UpdateClubBook saves clubBook when the stored version is still clubBook.Version and
	// increments the version. It returns ErrClubBookVersionConflict when the
	// version changed or the club book is gone, and ErrClubBookTitleTaken.
	UpdateClubBook(ctx context.Context, clubBook domain.ClubBook) (domain.ClubBook, error)
	// DeleteClubBook removes one of orgID's club books, or returns
	// ErrClubBookNotFound.
	DeleteClubBook(ctx context.Context, orgID, id string) error
	// InTx runs fn in one transaction: it commits when fn returns nil and
	// rolls back otherwise.
	InTx(ctx context.Context, fn func(tx Store) error) error
}
