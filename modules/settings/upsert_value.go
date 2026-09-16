package settings

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

const insertValueSQL = `
	INSERT INTO settings_values (key, value, version, updated_by, org_id)
	VALUES ($1, $2, 1, $3, $4)
	RETURNING ` + valueColumns

const updateValueSQL = `
	UPDATE settings_values
	SET value = $2, version = version + 1, updated_at = now(), updated_by = $3
	WHERE key = $1 AND org_id IS NULL
	RETURNING ` + valueColumns

const updateOrgValueSQL = `
	UPDATE settings_values
	SET value = $2, version = version + 1, updated_at = now(), updated_by = $3
	WHERE key = $1 AND org_id = $4
	RETURNING ` + valueColumns

// insertValue creates key's first row, platform-wide when orgID is empty.
// Two instances inserting at once violate a unique index; the loser gets
// ErrVersionConflict.
func insertValue(ctx context.Context, tx pgx.Tx, key, orgID string, value []byte, updatedBy string) (valueRow, error) {
	row, err := writeOne(ctx, tx, insertValueSQL, key, value, updatedBy, nullable(orgID))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return valueRow{}, ErrVersionConflict
	}
	return row, err
}

// updateValue replaces key's value and increments its version.
func updateValue(ctx context.Context, tx pgx.Tx, key, orgID string, value []byte, updatedBy string) (valueRow, error) {
	if orgID != "" {
		return writeOne(ctx, tx, updateOrgValueSQL, key, value, updatedBy, orgID)
	}
	return writeOne(ctx, tx, updateValueSQL, key, value, updatedBy)
}

func writeOne(ctx context.Context, tx pgx.Tx, sql, key string, value []byte, updatedBy string, args ...any) (valueRow, error) {
	rows, err := tx.Query(ctx, sql, append([]any{key, jsonbParam(value), updatedBy}, args...)...)
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

// nullable passes an empty organisation ID as SQL NULL: platform-wide.
func nullable(orgID string) any {
	if orgID == "" {
		return nil
	}
	return orgID
}

// notify tells every listening instance that key changed: the payload is
// the key, followed by a space and the organisation ID for an organisation
// value. PostgreSQL delivers it only if the transaction commits.
func notify(ctx context.Context, tx pgx.Tx, key, orgID string) error {
	payload := key
	if orgID != "" {
		payload += " " + orgID
	}
	_, err := tx.Exec(ctx, "SELECT pg_notify($1, $2)", notifyChannel, payload)
	return err
}
