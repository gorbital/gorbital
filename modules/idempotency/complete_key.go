package idempotency

import (
	"context"

	"gorbital.dev/modules/postgres"
)

// completeKeySQL stores the response of the request holding the lock.
const completeKeySQL = `
	UPDATE idempotency_keys
	SET status = $3, header = $4::jsonb, body = $5, lock_token = NULL, locked_until = NULL
	WHERE id = $1 AND lock_token = $2`

// completeKey returns how many rows it updated: 0 when the lock was lost.
func completeKey(ctx context.Context, db postgres.DBTX, id, token []byte, status int, header, body []byte) (int64, error) {
	tag, err := db.Exec(ctx, completeKeySQL, id, token, status, string(header), body)
	return tag.RowsAffected(), err
}
