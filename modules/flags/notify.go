package flags

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// notifyChannel carries the key of each changed flag.
const notifyChannel = "gorbital_flags"

// notify tells every listening instance that key changed. PostgreSQL
// delivers it only if the transaction commits.
func notify(ctx context.Context, tx pgx.Tx, key string) error {
	_, err := tx.Exec(ctx, "SELECT pg_notify($1, $2)", notifyChannel, key)
	return err
}
