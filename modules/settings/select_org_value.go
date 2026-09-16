package settings

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// selectOrgValuesSQL reads organisation values of the given keys only:
// rows of settings no longer declared OrgOverridable stay unread.
const selectOrgValuesSQL = `
	SELECT ` + valueColumns + `
	FROM settings_values
	WHERE org_id IS NOT NULL AND key = ANY($1)`

const selectOrgValueSQL = `
	SELECT ` + valueColumns + `
	FROM settings_values
	WHERE key = $1 AND org_id = $2`

const selectOrgValueForUpdateSQL = selectOrgValueSQL + `
	FOR UPDATE`

// selectOrgValues returns every organisation value of keys.
func selectOrgValues(ctx context.Context, db dbtx, keys []string) ([]valueRow, error) {
	rows, err := db.Query(ctx, selectOrgValuesSQL, keys)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanValueRow)
}

// selectOrgValue returns orgID's stored value of key and whether a row
// exists.
func selectOrgValue(ctx context.Context, db dbtx, orgID, key string) (valueRow, bool, error) {
	row, found, err := selectOne(ctx, db, selectOrgValueSQL, key, orgID)
	row.orgID = orgID
	return row, found, err
}

// selectOrgValueForUpdate locks orgID's row of key for the rest of the
// transaction. A missing row returns version 0.
func selectOrgValueForUpdate(ctx context.Context, tx pgx.Tx, orgID, key string) (valueRow, bool, error) {
	row, found, err := selectOne(ctx, tx, selectOrgValueForUpdateSQL, key, orgID)
	row.orgID = orgID
	return row, found, err
}
