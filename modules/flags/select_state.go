package flags

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

const selectStateSQL = `
	SELECT ` + stateColumns + `
	FROM flags_states
	WHERE key = $1`

// selectState returns key's stored state and whether a row exists. A
// missing row returns version 0.
func selectState(ctx context.Context, db dbtx, key string) (stateRow, bool, error) {
	return selectOneState(ctx, db, selectStateSQL, key)
}

// selectStateForUpdate is selectState locking the row for the rest of the
// transaction.
func selectStateForUpdate(ctx context.Context, tx pgx.Tx, key string) (stateRow, bool, error) {
	return selectOneState(ctx, tx, selectStateSQL+`
	FOR UPDATE`, key)
}

func selectOneState(ctx context.Context, db dbtx, sql, key string) (stateRow, bool, error) {
	rows, err := db.Query(ctx, sql, key)
	if err != nil {
		return stateRow{key: key}, false, err
	}
	row, err := pgx.CollectExactlyOneRow(rows, scanStateRow)
	if errors.Is(err, pgx.ErrNoRows) {
		return stateRow{key: key}, false, nil
	}
	return row, err == nil, err
}
