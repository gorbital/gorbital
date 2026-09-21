// Package repository stores the partners module's purchases in PostgreSQL
// with hand-written SQL, one file per operation. The table comes from
// db/migrations.
package repository

import (
	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/shelfie/internal/modules/partners/domain"
	"example.com/shelfie/internal/modules/partners/usecase"
)

// Store implements usecase.Store on a pool or a transaction.
type Store struct {
	db postgres.DBTX
}

var _ usecase.Store = (*Store)(nil)

// NewStore returns a store on db.
func NewStore(db postgres.DBTX) *Store { return &Store{db: db} }

// purchaseColumns are the columns scanPurchase reads, in its order.
const purchaseColumns = `id, partner, event_id, user_id, isbn, title, purchased_at, created_at`

func scanPurchase(row pgx.CollectableRow) (domain.Purchase, error) {
	var p domain.Purchase
	err := row.Scan(&p.ID, &p.Partner, &p.EventID, &p.UserID, &p.ISBN, &p.Title, &p.PurchasedAt, &p.CreatedAt)
	p.PurchasedAt, p.CreatedAt = p.PurchasedAt.UTC(), p.CreatedAt.UTC()
	return p, err
}
