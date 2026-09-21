// Package repository stores profiles in PostgreSQL with hand-written SQL,
// one file per operation. The table comes from db/migrations.
package repository

import (
	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/shelfie/internal/modules/profiles/domain"
	"example.com/shelfie/internal/modules/profiles/usecase"
)

// Store implements usecase.Store on a pool or a transaction.
type Store struct {
	db postgres.DBTX
}

var _ usecase.Store = (*Store)(nil)

// NewStore returns a store on db.
func NewStore(db postgres.DBTX) *Store { return &Store{db: db} }

const profileColumns = `user_id, display_name, country, suspended_at IS NOT NULL, created_at, updated_at`

func scanProfile(row pgx.CollectableRow) (domain.Profile, error) {
	var p domain.Profile
	err := row.Scan(&p.UserID, &p.DisplayName, &p.Country, &p.Suspended, &p.CreatedAt, &p.UpdatedAt)
	p.CreatedAt, p.UpdatedAt = p.CreatedAt.UTC(), p.UpdatedAt.UTC()
	return p, err
}
