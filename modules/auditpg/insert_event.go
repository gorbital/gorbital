package auditpg

import (
	"context"
	"time"

	"apistock.dev/modules/postgres"
)

// eventRow is a prepared event to insert into audit_events.
type eventRow struct {
	occurredAt   time.Time
	actorKind    string
	actorID      string
	actorLabel   string
	action       string
	resourceType string
	resourceID   string
	outcome      string
	orgID        string
	requestID    string
	traceID      string
	ip           string
	userAgent    string
	metadata     string
}

// Empty org_id and ip become NULL.
const insertEventSQL = `
	INSERT INTO audit_events (
		occurred_at, actor_kind, actor_id, actor_label, action, resource_type, resource_id,
		outcome, org_id, request_id, trace_id, ip, user_agent, metadata
	)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, ''), $10, $11, NULLIF($12, '')::inet, $13, $14::jsonb)`

// insertEvent stores one event.
func insertEvent(ctx context.Context, db postgres.DBTX, r eventRow) error {
	_, err := db.Exec(ctx, insertEventSQL,
		r.occurredAt, r.actorKind, r.actorID, r.actorLabel, r.action, r.resourceType, r.resourceID,
		r.outcome, r.orgID, r.requestID, r.traceID, r.ip, r.userAgent, r.metadata)
	return err
}
