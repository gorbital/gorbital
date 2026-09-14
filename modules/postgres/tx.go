package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// Beginner starts transactions. *pgxpool.Pool and *pgx.Conn implement it.
type Beginner interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// InTx runs fn in a read-write transaction with the server's default
// isolation level. See [InTxWithOptions].
func InTx(ctx context.Context, db Beginner, fn func(tx pgx.Tx) error) error {
	return InTxWithOptions(ctx, db, pgx.TxOptions{}, fn)
}

// InTxWithOptions runs fn in a transaction started with opts. It commits when
// fn returns nil. Otherwise it rolls back and returns fn's error unchanged, so
// callers can match domain errors with [errors.Is]. If fn panics, the
// transaction is rolled back and the panic continues.
//
// Build tx-bound repositories inside fn (for example NewUserStore(tx)); never
// keep tx after fn returns. With [pgx.Serializable], retry when [IsRetryable]
// reports true.
func InTxWithOptions(ctx context.Context, db Beginner, opts pgx.TxOptions, fn func(tx pgx.Tx) error) error {
	tx, err := db.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("postgres: begin transaction: %w", err)
	}
	// Roll back even when ctx is already cancelled.
	rollbackCtx := context.WithoutCancel(ctx)
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(rollbackCtx)
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(rollbackCtx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			return errors.Join(err, fmt.Errorf("postgres: rollback: %w", rbErr))
		}
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit: %w", err)
	}
	return nil
}
