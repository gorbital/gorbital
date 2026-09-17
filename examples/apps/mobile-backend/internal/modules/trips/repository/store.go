// Package repository stores trips in PostgreSQL with hand-written SQL, one
// file per operation. The table comes from db/migrations.
package repository

import (
	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/mobile-backend/internal/modules/trips/domain"
	"example.com/mobile-backend/internal/modules/trips/usecase"
)

// Store implements usecase.Store on a pool or a transaction.
type Store struct {
	db postgres.DBTX
}

var _ usecase.Store = (*Store)(nil)

// NewStore returns a store on db.
func NewStore(db postgres.DBTX) *Store { return &Store{db: db} }

// tripColumns are the columns scanTrip reads, in its order.
const tripColumns = `id, owner_id, destination, notes, created_at`

func scanTrip(row pgx.CollectableRow) (domain.Trip, error) {
	var t domain.Trip
	err := row.Scan(&t.ID, &t.OwnerID, &t.Destination, &t.Notes, &t.CreatedAt)
	t.CreatedAt = t.CreatedAt.UTC()
	return t, err
}
