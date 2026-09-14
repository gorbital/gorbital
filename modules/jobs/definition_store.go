package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// dbtx is the query subset of *pgxpool.Pool and pgx.Tx the manager uses.
type dbtx interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// definitionRow is one jobs_definitions row; nil fields use code defaults.
type definitionRow struct {
	name        string
	enabled     *bool
	schedule    *string
	timeoutMS   *int64
	maxAttempts *int32
	queue       *string
	priority    *int16
	version     int64
	updatedAt   time.Time
	updatedBy   string
}

const definitionColumns = `name, enabled, schedule, timeout_ms, max_attempts, queue, priority, version, updated_at, updated_by`

const selectDefinitionsSQL = `
	SELECT ` + definitionColumns + `
	FROM jobs_definitions
	WHERE org_id IS NULL`

const selectDefinitionSQL = selectDefinitionsSQL + ` AND name = $1`

const selectDefinitionForUpdateSQL = selectDefinitionSQL + `
	FOR UPDATE`

const insertDefinitionSQL = `
	INSERT INTO jobs_definitions (name, enabled, schedule, timeout_ms, max_attempts, queue, priority, version, updated_by)
	VALUES ($1, $2, $3, $4, $5, $6, $7, 1, $8)
	RETURNING ` + definitionColumns

const updateDefinitionSQL = `
	UPDATE jobs_definitions
	SET enabled = $2, schedule = $3, timeout_ms = $4, max_attempts = $5, queue = $6, priority = $7,
	    version = version + 1, updated_at = now(), updated_by = $8
	WHERE name = $1 AND org_id IS NULL
	RETURNING ` + definitionColumns

func scanDefinitionRow(row pgx.CollectableRow) (definitionRow, error) {
	var r definitionRow
	err := row.Scan(&r.name, &r.enabled, &r.schedule, &r.timeoutMS, &r.maxAttempts, &r.queue, &r.priority,
		&r.version, &r.updatedAt, &r.updatedBy)
	return r, err
}

// selectDefinitions returns every platform-wide override row.
func selectDefinitions(ctx context.Context, db dbtx) ([]definitionRow, error) {
	rows, err := db.Query(ctx, selectDefinitionsSQL)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanDefinitionRow)
}

// selectDefinition returns name's override row and whether it exists.
func selectDefinition(ctx context.Context, db dbtx, name string, forUpdate bool) (definitionRow, bool, error) {
	sql := selectDefinitionSQL
	if forUpdate {
		sql = selectDefinitionForUpdateSQL
	}
	rows, err := db.Query(ctx, sql, name)
	if err != nil {
		return definitionRow{}, false, err
	}
	row, err := pgx.CollectExactlyOneRow(rows, scanDefinitionRow)
	if errors.Is(err, pgx.ErrNoRows) {
		return definitionRow{name: name}, false, nil
	}
	return row, err == nil, err
}

// writeDefinition inserts name's first override row or updates it. A
// concurrent first insert violates the unique index and becomes
// ErrVersionConflict.
func writeDefinition(ctx context.Context, tx pgx.Tx, name string, o override, exists bool, updatedBy string) (definitionRow, error) {
	sql := insertDefinitionSQL
	if exists {
		sql = updateDefinitionSQL
	}
	p := o.params()
	rows, err := tx.Query(ctx, sql, name, p.enabled, p.schedule, p.timeoutMS, p.maxAttempts, p.queue, p.priority, updatedBy)
	if err != nil {
		return definitionRow{}, err
	}
	row, err := pgx.CollectExactlyOneRow(rows, scanDefinitionRow)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return definitionRow{}, ErrVersionConflict
	}
	return row, err
}

const insertDefinitionHistorySQL = `
	INSERT INTO jobs_definition_history (name, action, old_config, new_config, version, reason, actor_kind, actor_id, request_id)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

type historyRow struct {
	name      string
	action    string
	oldConfig json.RawMessage
	newConfig json.RawMessage
	version   int64
	reason    string
	actorKind string
	actorID   string
	requestID string
}

// insertDefinitionHistory records a change in the same transaction.
func insertDefinitionHistory(ctx context.Context, tx pgx.Tx, h historyRow) error {
	_, err := tx.Exec(ctx, insertDefinitionHistorySQL,
		h.name, h.action, string(h.oldConfig), string(h.newConfig), h.version, h.reason, h.actorKind, h.actorID, h.requestID)
	return err
}

const selectDefinitionHistorySQL = `
	SELECT id, name, action, old_config, new_config, version, reason, actor_kind, actor_id, request_id, changed_at
	FROM jobs_definition_history
	WHERE name = $1 AND org_id IS NULL AND ($2 = 0 OR id < $2)
	ORDER BY id DESC
	LIMIT $3`

// selectDefinitionHistory returns up to limit changes older than before.
func selectDefinitionHistory(ctx context.Context, db dbtx, name string, before int64, limit int) ([]DefinitionChange, error) {
	rows, err := db.Query(ctx, selectDefinitionHistorySQL, name, before, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (DefinitionChange, error) {
		var c DefinitionChange
		err := row.Scan(&c.ID, &c.Name, &c.Action, &c.OldConfig, &c.NewConfig, &c.Version, &c.Reason,
			&c.ActorKind, &c.ActorID, &c.RequestID, &c.ChangedAt)
		return c, err
	})
}

// notifyDefinition tells listening instances that name changed, on commit.
func notifyDefinition(ctx context.Context, tx pgx.Tx, name string) error {
	_, err := tx.Exec(ctx, "SELECT pg_notify($1, $2)", notifyChannel, name)
	return err
}

// overrideParams are an override's column values; nil is SQL NULL.
type overrideParams struct {
	enabled     *bool
	schedule    *string
	timeoutMS   *int64
	maxAttempts *int32
	queue       *string
	priority    *int16
}

func (o override) params() overrideParams {
	p := overrideParams{enabled: o.enabled, schedule: o.schedule, queue: o.queue}
	if o.timeout != nil {
		ms := o.timeout.Milliseconds()
		p.timeoutMS = &ms
	}
	if o.maxAttempts != nil {
		n := int32(*o.maxAttempts) //nolint:gosec // validated to 1..MaxAttemptsLimit
		p.maxAttempts = &n
	}
	if o.priority != nil {
		n := int16(*o.priority) //nolint:gosec // validated to 1..4
		p.priority = &n
	}
	return p
}

func (r definitionRow) override() override {
	o := override{
		enabled:   r.enabled,
		schedule:  r.schedule,
		queue:     r.queue,
		version:   r.version,
		updatedAt: r.updatedAt,
		updatedBy: r.updatedBy,
	}
	if r.timeoutMS != nil {
		d := time.Duration(*r.timeoutMS) * time.Millisecond
		o.timeout = &d
	}
	if r.maxAttempts != nil {
		n := int(*r.maxAttempts)
		o.maxAttempts = &n
	}
	if r.priority != nil {
		n := int(*r.priority)
		o.priority = &n
	}
	return o
}

// fieldsJSON encodes the overridden fields for history; {} means defaults.
func (o override) fieldsJSON() json.RawMessage {
	fields := map[string]any{}
	if o.enabled != nil {
		fields["enabled"] = *o.enabled
	}
	if o.schedule != nil {
		fields["schedule"] = *o.schedule
	}
	if o.timeout != nil {
		fields["timeout"] = o.timeout.String()
	}
	if o.maxAttempts != nil {
		fields["max_attempts"] = *o.maxAttempts
	}
	if o.queue != nil {
		fields["queue"] = *o.queue
	}
	if o.priority != nil {
		fields["priority"] = *o.priority
	}
	b, _ := json.Marshal(fields) // only strings, ints and bools
	return b
}

// isEmpty reports whether o overrides nothing.
func (o override) isEmpty() bool {
	return o.enabled == nil && o.schedule == nil && o.timeout == nil && o.maxAttempts == nil && o.queue == nil && o.priority == nil
}
