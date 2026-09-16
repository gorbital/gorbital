package settings

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/actor"
)

// historyRow is one change to insert into settings_history. An empty orgID
// is a platform-wide change.
type historyRow struct {
	key       string
	orgID     string
	oldValue  []byte
	newValue  []byte
	version   int64
	reason    string
	actorKind string
	actorID   string
	requestID string
}

const insertHistorySQL = `
	INSERT INTO settings_history (key, old_value, new_value, version, reason, actor_kind, actor_id, request_id, org_id)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

// insertHistory records a change in the same transaction as the value.
func insertHistory(ctx context.Context, tx pgx.Tx, h historyRow) error {
	_, err := tx.Exec(ctx, insertHistorySQL,
		h.key, jsonbParam(h.oldValue), jsonbParam(h.newValue), h.version, h.reason, h.actorKind, h.actorID, h.requestID, nullable(h.orgID))
	return err
}

const historyColumns = `id, key, coalesce(org_id, ''), old_value, new_value, version, reason, actor_kind, actor_id, request_id, changed_at`

// selectHistorySQL pages by id: $2 = 0 starts from the newest entry.
const selectHistorySQL = `
	SELECT ` + historyColumns + `
	FROM settings_history
	WHERE key = $1 AND org_id IS NULL AND ($2 = 0 OR id < $2)
	ORDER BY id DESC
	LIMIT $3`

// selectOrgHistorySQL is selectHistorySQL for one organisation's changes.
const selectOrgHistorySQL = `
	SELECT ` + historyColumns + `
	FROM settings_history
	WHERE key = $1 AND org_id = $4 AND ($2 = 0 OR id < $2)
	ORDER BY id DESC
	LIMIT $3`

// selectHistory returns up to limit changes to key older than before: the
// platform-wide changes when orgID is empty, else the organisation's.
func selectHistory(ctx context.Context, db dbtx, key, orgID string, before int64, limit int) ([]HistoryEntry, error) {
	sql, args := selectHistorySQL, []any{key, before, limit}
	if orgID != "" {
		sql, args = selectOrgHistorySQL, append(args, orgID)
	}
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (HistoryEntry, error) {
		var e HistoryEntry
		var kind string
		err := row.Scan(&e.ID, &e.Key, &e.OrgID, &e.OldValue, &e.NewValue, &e.Version, &e.Reason, &kind, &e.ActorID, &e.RequestID, &e.ChangedAt)
		e.ActorKind = actor.Kind(kind)
		return e, err
	})
}
