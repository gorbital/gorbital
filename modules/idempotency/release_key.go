package idempotency

import (
	"context"

	"gorbital.dev/modules/postgres"
)

// releaseKeySQL deletes a key only while the releasing request holds it.
const releaseKeySQL = `DELETE FROM idempotency_keys WHERE id = $1 AND lock_token = $2`

func releaseKey(ctx context.Context, db postgres.DBTX, id, token []byte) error {
	_, err := db.Exec(ctx, releaseKeySQL, id, token)
	return err
}
