package releases

import (
	"context"
	"time"

	"gorbital.dev/modules/postgres"
)

const deleteInstancesSeenBeforeSQL = `DELETE FROM release_instances WHERE last_seen_at < $1`

// deleteInstancesSeenBefore removes instances last seen before before,
// whether they stopped cleanly or not.
func deleteInstancesSeenBefore(ctx context.Context, db postgres.DBTX, before time.Time) (int64, error) {
	tag, err := db.Exec(ctx, deleteInstancesSeenBeforeSQL, before)
	return tag.RowsAffected(), err
}
