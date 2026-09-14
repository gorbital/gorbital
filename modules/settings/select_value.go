package settings

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// valueRow is one settings_values row. A nil value means the default.
type valueRow struct {
	key       string
	value     []byte
	version   int64
	updatedAt time.Time
	updatedBy string
}

const valueColumns = `key, value, version, updated_at, updated_by`

const selectValuesSQL = `
	SELECT ` + valueColumns + `
	FROM settings_values
	WHERE org_id IS NULL`

const selectValueSQL = `
	SELECT ` + valueColumns + `
	FROM settings_values
	WHERE key = $1 AND org_id IS NULL`

const selectValueForUpdateSQL = selectValueSQL + `
	FOR UPDATE`

func scanValueRow(row pgx.CollectableRow) (valueRow, error) {
	var r valueRow
	err := row.Scan(&r.key, &r.value, &r.version, &r.updatedAt, &r.updatedBy)
	return r, err
}

// selectValues returns every platform-wide stored value.
func selectValues(ctx context.Context, db dbtx) ([]valueRow, error) {
	rows, err := db.Query(ctx, selectValuesSQL)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanValueRow)
}

// selectValue returns key's stored value and whether a row exists.
func selectValue(ctx context.Context, db dbtx, key string) (valueRow, bool, error) {
	return selectOne(ctx, db, selectValueSQL, key)
}

// selectValueForUpdate locks key's row for the rest of the transaction. A
// missing row returns version 0.
func selectValueForUpdate(ctx context.Context, tx pgx.Tx, key string) (valueRow, bool, error) {
	return selectOne(ctx, tx, selectValueForUpdateSQL, key)
}

func selectOne(ctx context.Context, db dbtx, sql, key string) (valueRow, bool, error) {
	rows, err := db.Query(ctx, sql, key)
	if err != nil {
		return valueRow{key: key}, false, err
	}
	row, err := pgx.CollectExactlyOneRow(rows, scanValueRow)
	if errors.Is(err, pgx.ErrNoRows) {
		return valueRow{key: key}, false, nil
	}
	return row, err == nil, err
}
