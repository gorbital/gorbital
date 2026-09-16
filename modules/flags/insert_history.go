package flags

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// historyRow is one change to insert into flags_history.
type historyRow struct {
	key       string
	oldState  []byte
	newState  []byte
	version   int64
	reason    string
	actorKind string
	actorID   string
	requestID string
}

const insertHistorySQL = `
	INSERT INTO flags_history (key, old_state, new_state, version, reason, actor_kind, actor_id, request_id)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

// insertHistory records a change in the same transaction as the state.
func insertHistory(ctx context.Context, tx pgx.Tx, h historyRow) error {
	_, err := tx.Exec(ctx, insertHistorySQL,
		h.key, jsonbParam(h.oldState), jsonbParam(h.newState), h.version, h.reason, h.actorKind, h.actorID, h.requestID)
	return err
}
