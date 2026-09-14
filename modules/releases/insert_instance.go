package releases

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"apistock.dev/modules/postgres"
)

type instanceRow struct {
	instanceID string
	version    string
	commit     string
	buildTime  *time.Time
	modified   bool
	goVersion  string
	host       string
	startedAt  time.Time
}

const insertInstanceSQL = `
	INSERT INTO release_instances (instance_id, version, commit, build_time, modified, go_version, host, started_at, last_seen_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
	RETURNING id`

// insertInstance records an instance start and returns its row ID.
func insertInstance(ctx context.Context, db postgres.DBTX, r instanceRow) (int64, error) {
	rows, err := db.Query(ctx, insertInstanceSQL,
		r.instanceID, r.version, r.commit, r.buildTime, r.modified, r.goVersion, r.host, r.startedAt)
	if err != nil {
		return 0, err
	}
	return pgx.CollectExactlyOneRow(rows, pgx.RowTo[int64])
}
