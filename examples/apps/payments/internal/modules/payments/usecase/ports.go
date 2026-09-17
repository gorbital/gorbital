package usecase

import (
	"context"

	"github.com/riverqueue/river"

	"example.com/payments/internal/modules/payments/domain"
)

// docs:start ports

// Store reads and writes payments; repository.Store implements it with SQL.
type Store interface {
	// InsertPayment records p unless its EventID is already recorded. It
	// returns the payment now in the table and whether this call inserted
	// it, or domain.ErrDeliveryInProgress when another transaction is
	// inserting the same delivery.
	InsertPayment(ctx context.Context, p domain.Payment) (domain.Payment, bool, error)
	// SelectPayment returns the payment with the ID, or
	// domain.ErrPaymentNotFound.
	SelectPayment(ctx context.Context, id string) (domain.Payment, error)
}

// Tx is a Store bound to one transaction, and the jobs enqueued in it.
type Tx interface {
	Store
	// Enqueue adds a job that workers see only if the transaction commits.
	Enqueue(ctx context.Context, args river.JobArgs) error
}

// TxManager runs a use case's writes in one transaction: everything written
// through the Tx commits together, or none of it does. Use cases never
// import pgx (ADR-0022); repository.TxManager implements this with
// postgres.InTx.
type TxManager interface {
	InTx(ctx context.Context, fn func(tx Tx) error) error
}

// docs:end ports
