// Package repository stores payments in PostgreSQL with hand-written SQL,
// one file per operation. The table comes from db/migrations.
package repository

import (
	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"

	"example.com/payments/internal/modules/payments/domain"
	"example.com/payments/internal/modules/payments/usecase"
)

// Store implements usecase.Store on a pool or a transaction.
type Store struct {
	db postgres.DBTX
}

var _ usecase.Store = (*Store)(nil)

// NewStore returns a store on db.
func NewStore(db postgres.DBTX) *Store { return &Store{db: db} }

// paymentColumns are the columns scanPayment reads, in its order.
const paymentColumns = `id, event_id, provider_payment_id, amount_minor, currency, status, occurred_at, recorded_at`

func scanPayment(row pgx.CollectableRow) (domain.Payment, error) {
	var p domain.Payment
	var status string
	err := row.Scan(&p.ID, &p.EventID, &p.ProviderPaymentID, &p.AmountMinor, &p.Currency, &status, &p.OccurredAt, &p.RecordedAt)
	p.Status = domain.Status(status)
	p.OccurredAt, p.RecordedAt = p.OccurredAt.UTC(), p.RecordedAt.UTC()
	return p, err
}

// insertedPayment is a payment and whether the statement inserted it.
type insertedPayment struct {
	payment domain.Payment
	applied bool
}

func scanInsertedPayment(row pgx.CollectableRow) (insertedPayment, error) {
	var p domain.Payment
	var status string
	var applied bool
	err := row.Scan(&p.ID, &p.EventID, &p.ProviderPaymentID, &p.AmountMinor, &p.Currency, &status, &p.OccurredAt, &p.RecordedAt, &applied)
	p.Status = domain.Status(status)
	p.OccurredAt, p.RecordedAt = p.OccurredAt.UTC(), p.RecordedAt.UTC()
	return insertedPayment{payment: p, applied: applied}, err
}
