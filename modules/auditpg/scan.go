package auditpg

import (
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"

	"apistock.dev/actor"
	"apistock.dev/audit"
)

// StoredEvent is a recorded audit event.
type StoredEvent struct {
	// ID increases in recording order.
	ID int64
	// RecordedAt is when the database stored the event, by its clock;
	// OccurredAt is when the action happened, by the recording instance's.
	// Both are UTC.
	RecordedAt time.Time
	audit.Event
}

const eventColumns = `id, occurred_at, recorded_at, actor_kind, actor_id, actor_label, action,
	resource_type, resource_id, outcome, COALESCE(org_id, ''), request_id, trace_id,
	COALESCE(host(ip), ''), user_agent, metadata`

func scanEvent(row pgx.CollectableRow) (StoredEvent, error) {
	var (
		e                  StoredEvent
		actorKind, outcome string
		metadata           []byte
	)
	err := row.Scan(&e.ID, &e.OccurredAt, &e.RecordedAt, &actorKind, &e.ActorID, &e.ActorLabel, &e.Action,
		&e.ResourceType, &e.ResourceID, &outcome, &e.OrgID, &e.RequestID, &e.TraceID,
		&e.IP, &e.UserAgent, &metadata)
	if err != nil {
		return StoredEvent{}, err
	}
	e.ActorKind, e.Outcome = actor.Kind(actorKind), audit.Outcome(outcome)
	e.OccurredAt, e.RecordedAt = e.OccurredAt.UTC(), e.RecordedAt.UTC()
	if err := json.Unmarshal(metadata, &e.Metadata); err != nil {
		return StoredEvent{}, err
	}
	return e, nil
}
