package flags

import (
	"context"

	"github.com/jackc/pgx/v5"

	"gorbital.dev/actor"
)

// selectHistorySQL pages by id: $2 = 0 starts from the newest entry.
const selectHistorySQL = `
	SELECT id, key, old_state, new_state, version, reason, actor_kind, actor_id, request_id, changed_at
	FROM flags_history
	WHERE key = $1 AND ($2 = 0 OR id < $2)
	ORDER BY id DESC
	LIMIT $3`

// historyEntryRow is a flags_history row before its states are decoded.
type historyEntryRow struct {
	HistoryEntry
	oldState, newState []byte
}

// selectHistory returns up to limit changes to key older than before.
func selectHistory(ctx context.Context, db dbtx, key string, before int64, limit int) ([]historyEntryRow, error) {
	rows, err := db.Query(ctx, selectHistorySQL, key, before, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (historyEntryRow, error) {
		var e historyEntryRow
		var kind string
		err := row.Scan(&e.ID, &e.Key, &e.oldState, &e.newState, &e.Version, &e.Reason, &kind, &e.ActorID, &e.RequestID, &e.ChangedAt)
		e.ActorKind = actor.Kind(kind)
		return e, err
	})
}
