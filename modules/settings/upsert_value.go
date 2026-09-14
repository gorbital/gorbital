package settings

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const insertValueSQL = `
	INSERT INTO settings_values (key, value, version, updated_by)
	VALUES ($1, $2, 1, $3)
	RETURNING ` + valueColumns

const updateValueSQL = `
	UPDATE settings_values
	SET value = $2, version = version + 1, updated_at = now(), updated_by = $3
	WHERE key = $1 AND org_id IS NULL
	RETURNING ` + valueColumns

// insertValue creates key's first row. Two instances inserting at once
// violate the unique index; the loser gets ErrVersionConflict.
func insertValue(ctx context.Context, tx pgx.Tx, key string, value []byte, updatedBy string) (valueRow, error) {
	row, err := writeOne(ctx, tx, insertValueSQL, key, value, updatedBy)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return valueRow{}, ErrVersionConflict
	}
	return row, err
}

// updateValue replaces key's value and increments its version.
func updateValue(ctx context.Context, tx pgx.Tx, key string, value []byte, updatedBy string) (valueRow, error) {
	return writeOne(ctx, tx, updateValueSQL, key, value, updatedBy)
}

func writeOne(ctx context.Context, tx pgx.Tx, sql, key string, value []byte, updatedBy string) (valueRow, error) {
	rows, err := tx.Query(ctx, sql, key, jsonbParam(value), updatedBy)
	if err != nil {
		return valueRow{}, err
	}
	return pgx.CollectExactlyOneRow(rows, scanValueRow)
}

// jsonbParam passes JSON as text, and nil as SQL NULL rather than the JSON
// literal null.
func jsonbParam(value []byte) any {
	if value == nil {
		return nil
	}
	return string(value)
}

// notify tells every listening instance that key changed. PostgreSQL
// delivers it only if the transaction commits.
func notify(ctx context.Context, tx pgx.Tx, key string) error {
	_, err := tx.Exec(ctx, "SELECT pg_notify($1, $2)", notifyChannel, key)
	return err
}
