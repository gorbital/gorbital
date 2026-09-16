package flags

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const insertStateSQL = `
	INSERT INTO flags_states (key, state, version, updated_by)
	VALUES ($1, $2, 1, $3)
	RETURNING ` + stateColumns

// insertState creates key's first row. Two instances inserting at once
// violate the primary key; the loser gets ErrVersionConflict.
func insertState(ctx context.Context, tx pgx.Tx, key string, state []byte, updatedBy string) (stateRow, error) {
	rows, err := tx.Query(ctx, insertStateSQL, key, jsonbParam(state), updatedBy)
	if err != nil {
		return stateRow{}, err
	}
	row, err := pgx.CollectExactlyOneRow(rows, scanStateRow)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return stateRow{}, ErrVersionConflict
	}
	return row, err
}

// jsonbParam passes JSON as text, and nil as SQL NULL rather than the JSON
// literal null.
func jsonbParam(value []byte) any {
	if value == nil {
		return nil
	}
	return string(value)
}
