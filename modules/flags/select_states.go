package flags

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// stateRow is one flags_states row. A nil state means the declared default.
type stateRow struct {
	key       string
	state     []byte
	version   int64
	updatedAt time.Time
	updatedBy string
}

const stateColumns = `key, state, version, updated_at, updated_by`

const selectStatesSQL = `
	SELECT ` + stateColumns + `
	FROM flags_states`

func scanStateRow(row pgx.CollectableRow) (stateRow, error) {
	var r stateRow
	err := row.Scan(&r.key, &r.state, &r.version, &r.updatedAt, &r.updatedBy)
	return r, err
}

// selectStates returns every stored state.
func selectStates(ctx context.Context, db dbtx) ([]stateRow, error) {
	rows, err := db.Query(ctx, selectStatesSQL)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanStateRow)
}
