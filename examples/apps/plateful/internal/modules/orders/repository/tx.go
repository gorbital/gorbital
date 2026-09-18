package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorbital.dev/modules/jobs"
	"gorbital.dev/modules/postgres"

	"example.com/plateful/internal/modules/notifications"
	"example.com/plateful/internal/modules/orders/usecase"
)

// docs:start tx-manager

// TxManager implements usecase.TxManager: it runs a use case's writes in one
// transaction, and enqueues that transaction's jobs in it.
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
// and rolling back otherwise, panics included. The transaction is never kept
// after fn returns.
func (m *TxManager) InTx(ctx context.Context, fn func(tx usecase.Tx) error) error {
	return postgres.InTx(ctx, m.db, func(tx pgx.Tx) error {
		return fn(&boundTx{Store: newStoreOn(tx), tx: tx, jobs: m.jobs})
	})
}

// boundTx is the Store of one transaction, plus that transaction's jobs.
type boundTx struct {
	*Store
	tx   pgx.Tx
	jobs *jobs.Client
}

// Notify enqueues a notification for the restaurant's organisation in the
// transaction. River writes the job row through tx, so a worker can pick it
// up only once the transaction has committed, and a rollback takes the job
// with it — which is the whole reason placing an order and telling the
// restaurant about it are one operation and not two.
//
// notifications is another module. This takes its root package, which is
// what a module may use of another (internal/modules/architecture_test.go),
// and never its layers.
func (b *boundTx) Notify(ctx context.Context, orgID, title string, lines []string) error {
	_, err := b.jobs.InsertTx(ctx, b.tx, notifications.Fanout(orgID, title, lines), nil)
	return err
}

// docs:end tx-manager
