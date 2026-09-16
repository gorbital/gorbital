package flags

import (
	"context"

	"github.com/jackc/pgx/v5"
)

const updateStateSQL = `
	UPDATE flags_states
	SET state = $2, version = version + 1, updated_at = now(), updated_by = $3
	WHERE key = $1
	RETURNING ` + stateColumns

// updateState replaces key's state and increments its version.
func updateState(ctx context.Context, tx pgx.Tx, key string, state []byte, updatedBy string) (stateRow, error) {
	rows, err := tx.Query(ctx, updateStateSQL, key, jsonbParam(state), updatedBy)
	if err != nil {
		return stateRow{}, err
	}
	return pgx.CollectExactlyOneRow(rows, scanStateRow)
}
