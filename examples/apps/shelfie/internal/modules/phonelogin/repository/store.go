// Package repository stores phone numbers and codes in PostgreSQL with
// hand-written SQL, one file per operation. The tables come from
// db/migrations.
package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/modules/postgres"

	"example.com/shelfie/internal/modules/phonelogin/usecase"
)

// Store implements usecase.Store on the pool, or on a transaction inside
// InTx.
type Store struct {
	db   postgres.DBTX
	pool *pgxpool.Pool // nil inside a transaction
}

var _ usecase.Store = (*Store)(nil)

// NewStore returns a store on pool.
func NewStore(pool *pgxpool.Pool) *Store { return &Store{db: pool, pool: pool} }

// InTx runs fn with a store bound to one transaction.
func (s *Store) InTx(ctx context.Context, fn func(tx usecase.Store) error) error {
	if s.pool == nil {
		return fn(s)
	}
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error { return fn(&Store{db: tx}) })
}
