// Package repository stores the auth module's accounts, sessions, codes and
// role assignments in PostgreSQL with hand-written SQL, one file per
// operation (ADR-0032). The tables come from db/migrations.
package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/modules/postgres"

	authusecase "example.com/admin-tool/internal/modules/auth/usecase"
)

// Store implements the auth use cases' storage ports. It runs on the pool,
// or on a transaction inside InTx.
type Store struct {
	db   postgres.DBTX
	pool *pgxpool.Pool // nil inside a transaction
}

var _ authusecase.Store = (*Store)(nil)

// NewStore returns a store on pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{db: pool, pool: pool}
}

// InTx runs fn with a store bound to one transaction. Inside a transaction,
// fn joins it.
func (s *Store) InTx(ctx context.Context, fn func(tx authusecase.Store) error) error {
	if s.pool == nil {
		return fn(s)
	}
	return postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(&Store{db: tx})
	})
}

// Tx returns the transaction a store from InTx runs on, for the app's hooks
// that write in sign-in's transaction; false for a store on the pool.
func Tx(store authusecase.Store) (pgx.Tx, bool) {
	s, ok := store.(*Store)
	if !ok || s.pool != nil {
		return nil, false
	}
	tx, ok := s.db.(pgx.Tx)
	return tx, ok
}
