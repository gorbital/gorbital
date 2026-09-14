package auditpg

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"apistock.dev/modules/postgres"
)

const selectEventSQL = `SELECT ` + eventColumns + ` FROM audit_events WHERE id = $1`

// Get returns one event, or [ErrEventNotFound].
func (s *Store) Get(ctx context.Context, id int64) (StoredEvent, error) {
	e, err := selectEvent(ctx, s.pool, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredEvent{}, ErrEventNotFound
	}
	if err != nil {
		return StoredEvent{}, fmt.Errorf("auditpg: get event %d: %v", id, err) //nolint:errorlint // driver errors aren't API (ADR-0018)
	}
	return e, nil
}

func selectEvent(ctx context.Context, db postgres.DBTX, id int64) (StoredEvent, error) {
	rows, err := db.Query(ctx, selectEventSQL, id)
	if err != nil {
		return StoredEvent{}, err
	}
	return pgx.CollectExactlyOneRow(rows, scanEvent)
}
