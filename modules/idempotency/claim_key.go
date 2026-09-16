package idempotency

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/modules/postgres"
)

// claimKeySQL inserts an in-progress key, or takes over an existing one that
// expired (older than the retention, whatever its fingerprint) or whose lock
// passed its TTL without a stored response (the same fingerprint only: a
// different request is refused as a reused key). It returns a row only when
// the key was claimed.
//
// $1 id, $2 fingerprint, $3 lock token, $4 lock TTL and $5 retention in
// microseconds, $6 now (NULL: the database clock).
const claimKeySQL = `
	WITH clock AS (
		SELECT COALESCE($6::timestamptz, statement_timestamp()) AS now
	)
	INSERT INTO idempotency_keys AS k (id, fingerprint, lock_token, locked_until, created_at)
	SELECT $1, $2, $3, clock.now + $4::bigint * interval '1 microsecond', clock.now FROM clock
	ON CONFLICT (id) DO UPDATE
	SET fingerprint = EXCLUDED.fingerprint, lock_token = EXCLUDED.lock_token, locked_until = EXCLUDED.locked_until,
		created_at = EXCLUDED.created_at, status = NULL, header = NULL, body = NULL
	WHERE k.created_at <= EXCLUDED.created_at - $5::bigint * interval '1 microsecond'
		OR (k.status IS NULL AND k.locked_until <= EXCLUDED.created_at AND k.fingerprint = EXCLUDED.fingerprint)
	RETURNING true`

type claimArgs struct {
	id, fingerprint, token []byte
	lockTTL, retention     time.Duration
	now                    *time.Time
}

// claimKey reports whether the key was claimed.
func claimKey(ctx context.Context, db postgres.DBTX, a claimArgs) (bool, error) {
	var claimed bool
	err := db.QueryRow(ctx, claimKeySQL, a.id, a.fingerprint, a.token,
		a.lockTTL.Microseconds(), a.retention.Microseconds(), a.now).Scan(&claimed)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return claimed, err
}
