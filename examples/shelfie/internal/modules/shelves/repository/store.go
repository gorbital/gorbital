// Package repository stores the shelves module's shelves in PostgreSQL
// with hand-written SQL, one file per operation. The table comes from
// db/migrations.
package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/modules/postgres"

	"example.com/shelfie/internal/modules/shelves/domain"
	"example.com/shelfie/internal/modules/shelves/usecase"
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

// shelfColumns are the columns scanShelf reads, in its order.
const shelfColumns = `id, owner_id, name, description, visibility, version, created_at, updated_at`

func scanShelf(row pgx.CollectableRow) (domain.Shelf, error) {
	var shelf domain.Shelf
	err := row.Scan(&shelf.ID, &shelf.OwnerID, &shelf.Name, &shelf.Description, &shelf.Visibility, &shelf.Version, &shelf.CreatedAt, &shelf.UpdatedAt)
	shelf.CreatedAt, shelf.UpdatedAt = shelf.CreatedAt.UTC(), shelf.UpdatedAt.UTC()
	return shelf, err
}

// constraintError turns the constraint violations the use cases handle into
// domain errors.
func constraintError(err error) error {
	if constraint, ok := postgres.UniqueViolation(err); ok {
		switch constraint {
		case "shelves_owner_name":
			return domain.ErrShelfNameTaken
		}
	}
	return err
}
