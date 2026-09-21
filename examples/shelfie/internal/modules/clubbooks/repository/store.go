// Package repository stores the club books module's club books in PostgreSQL
// with hand-written SQL, one file per operation. The table comes from
// db/migrations.
package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/modules/postgres"

	"example.com/shelfie/internal/modules/clubbooks/domain"
	"example.com/shelfie/internal/modules/clubbooks/usecase"
)

// Store implements usecase.Store. It runs on the pool, or on a transaction
// inside InTx.
type Store struct {
	db   postgres.DBTX
	pool *pgxpool.Pool // nil inside a transaction
}

var _ usecase.Store = (*Store)(nil)

// NewStore returns a store on pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{db: pool, pool: pool}
}

// InTx runs fn with a store bound to one transaction. Inside a transaction,
// fn joins it.
func (s *Store) InTx(ctx context.Context, fn func(tx usecase.Store) error) error {
	if s.pool == nil {
		return fn(s)
	}
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(&Store{db: tx})
	})
}

// clubBookColumns are the columns scanClubBook reads, in its order.
const clubBookColumns = `id, org_id, created_by, title, author, status, note, version, created_at, updated_at`

func scanClubBook(row pgx.CollectableRow) (domain.ClubBook, error) {
	var clubBook domain.ClubBook
	err := row.Scan(&clubBook.ID, &clubBook.OrgID, &clubBook.CreatedBy, &clubBook.Title, &clubBook.Author, &clubBook.Status, &clubBook.Note, &clubBook.Version, &clubBook.CreatedAt, &clubBook.UpdatedAt)
	clubBook.CreatedAt, clubBook.UpdatedAt = clubBook.CreatedAt.UTC(), clubBook.UpdatedAt.UTC()
	return clubBook, err
}

// constraintError turns the constraint violations the use cases handle into
// domain errors.
func constraintError(err error) error {
	if constraint, ok := postgres.UniqueViolation(err); ok {
		switch constraint {
		case "club_books_org_title":
			return domain.ErrClubBookTitleTaken
		}
	}
	return err
}
