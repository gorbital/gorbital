package settings

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// selectOverridesSQL pages by organisation ID: an empty $2 starts from the
// first. Rows reset to the default (a NULL value) aren't overrides.
const selectOverridesSQL = `
	SELECT ` + valueColumns + `
	FROM settings_values
	WHERE key = $1 AND org_id IS NOT NULL AND value IS NOT NULL AND org_id > $2
	ORDER BY org_id
	LIMIT $3`

// selectOverrides returns up to limit organisation values of key, by
// organisation ID after after.
func selectOverrides(ctx context.Context, db dbtx, key, after string, limit int) ([]valueRow, error) {
	rows, err := db.Query(ctx, selectOverridesSQL, key, after, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanValueRow)
}
