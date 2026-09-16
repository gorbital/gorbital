package idempotency

import (
	"context"

	"gorbital.dev/modules/postgres"
)

const selectKeySQL = `SELECT fingerprint, status, header, body FROM idempotency_keys WHERE id = $1`

// keyRow is a key that couldn't be claimed.
type keyRow struct {
	fingerprint []byte
	status      *int16 // nil while in progress
	header      []byte
	body        []byte
}

// selectKey reads a key; pgx.ErrNoRows when it doesn't exist.
func selectKey(ctx context.Context, db postgres.DBTX, id []byte) (keyRow, error) {
	var r keyRow
	err := db.QueryRow(ctx, selectKeySQL, id).Scan(&r.fingerprint, &r.status, &r.header, &r.body)
	return r, err
}
