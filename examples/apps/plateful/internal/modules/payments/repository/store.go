// Package repository stores the payments module's payments and the
// provider's events in PostgreSQL with hand-written SQL, one file per
// operation. The tables come from db/migrations.
//
// One file here, select_order.go, reads a table this module doesn't own.
// That is allowed and expected: a module never imports another module's Go
// packages, but the database is shared, and a read with SQL is how a rule
// that spans two modules is written.
package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/payments/domain"
	"example.com/plateful/internal/modules/payments/usecase"
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

// paymentColumns are the columns scanPayment reads, in its order. The names
// are public API: the orders module reads order_payments.status with SQL of
// its own.
const paymentColumns = `id, org_id, order_id, customer_id, amount_minor, currency, status, ` +
	`provider_ref, failure_reason, version, created_at, updated_at`

func scanPayment(row pgx.CollectableRow) (domain.Payment, error) {
	var p domain.Payment
	err := row.Scan(&p.ID, &p.OrgID, &p.OrderID, &p.CustomerID, &p.AmountMinor, &p.Currency,
		&p.Status, &p.ProviderRef, &p.FailureReason, &p.Version, &p.CreatedAt, &p.UpdatedAt)
	p.CreatedAt, p.UpdatedAt = p.CreatedAt.UTC(), p.UpdatedAt.UTC()
	return p, err
}

// forUpdate locks the rows a select returns until the transaction ends.
const forUpdate = ` FOR UPDATE`

// constraintError turns the constraint violations the use cases handle into
// domain errors.
func constraintError(err error) error {
	if constraint, ok := postgres.UniqueViolation(err); ok && constraint == "order_payments_order_id_key" {
		// The order already has a payment: two pay calls raced, and the
		// loser is told what it would have been told had it read first.
		return domain.ErrPaymentNotPayable
	}
	if _, ok := postgres.ForeignKeyViolation(err); ok {
		// The order is gone, or belongs to another organisation than the
		// payment claims.
		return domain.ErrOrderNotFound
	}
	return err
}
