// Package repository stores the images module's rows in PostgreSQL with
// hand-written SQL, one file per operation. The table comes from
// db/migrations. The files themselves are not here: they live in the
// storage bucket, and this package only keeps the key that names them.
package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/images/domain"
	"example.com/plateful/internal/modules/images/usecase"
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

// imageColumns are the columns scanImage reads, in its order.
const imageColumns = `id, org_id, created_by, purpose, storage_key, content_type, ` +
	`size_bytes, status, created_at, updated_at`

func scanImage(row pgx.CollectableRow) (domain.Image, error) {
	var i domain.Image
	err := row.Scan(&i.ID, &i.OrgID, &i.CreatedBy, &i.Purpose, &i.StorageKey, &i.ContentType,
		&i.SizeBytes, &i.Status, &i.CreatedAt, &i.UpdatedAt)
	i.CreatedAt, i.UpdatedAt = i.CreatedAt.UTC(), i.UpdatedAt.UTC()
	return i, err
}
