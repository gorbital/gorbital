package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/postgres"

	"example.com/payments/internal/modules/payments/usecase"
)

// docs:start tx-manager

// TxManager implements usecase.TxManager: it runs a use case's writes in
// one transaction, and enqueues the jobs of that transaction in it.
type TxManager struct {
	db   *pgxpool.Pool
	jobs *jobs.Client
}

var _ usecase.TxManager = (*TxManager)(nil)

// NewTxManager returns a manager on db that enqueues through client.
func NewTxManager(db *pgxpool.Pool, client *jobs.Client) *TxManager {
	return &TxManager{db: db, jobs: client}
}

// InTx runs fn in a read-write transaction, committing when it returns nil
// and rolling back otherwise, panics included. The tx is never kept after
// fn returns.
func (m *TxManager) InTx(ctx context.Context, fn func(tx usecase.Tx) error) error {
	return postgres.InTx(ctx, m.db, func(tx pgx.Tx) error {
		return fn(&boundTx{Store: NewStore(tx), tx: tx, jobs: m.jobs})
	})
}

// boundTx is the Store of one transaction, plus that transaction's jobs.
type boundTx struct {
	*Store
	tx   pgx.Tx
	jobs *jobs.Client
}

// Enqueue adds a job in the transaction: River writes the row through tx, so
// a worker can pick the job up only once the transaction has committed, and
// a rollback takes the job with it.
func (b *boundTx) Enqueue(ctx context.Context, args river.JobArgs) error {
	_, err := b.jobs.InsertTx(ctx, b.tx, args, nil)
	return err
}

// docs:end tx-manager
