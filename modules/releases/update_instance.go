package releases

import (
	"context"
	"time"

	"apistock.dev/modules/postgres"
)

const (
	touchInstanceSQL = `UPDATE release_instances SET last_seen_at = $2 WHERE id = $1 AND stopped_at IS NULL`
	stopInstanceSQL  = `UPDATE release_instances SET last_seen_at = $2, stopped_at = $2 WHERE id = $1 AND stopped_at IS NULL`
)

// touchInstance records a heartbeat and reports whether the running instance
// still exists.
func touchInstance(ctx context.Context, db postgres.DBTX, id int64, now time.Time) (bool, error) {
	tag, err := db.Exec(ctx, touchInstanceSQL, id, now)
	return tag.RowsAffected() == 1, err
}

// stopInstance marks an instance stopped.
func stopInstance(ctx context.Context, db postgres.DBTX, id int64, now time.Time) error {
	_, err := db.Exec(ctx, stopInstanceSQL, id, now)
	return err
}
