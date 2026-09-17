-- Audit events (ADR-0036). Rows are append-only: a trigger rejects updates.
-- Rows are removed only by retention, which deletes whole rows by age.

-- +goose Up
CREATE TABLE audit_events (
    id            bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    occurred_at   timestamptz NOT NULL,
    recorded_at   timestamptz NOT NULL DEFAULT now(),
    actor_kind    text        NOT NULL,
    actor_id      text        NOT NULL DEFAULT '',
    actor_label   text        NOT NULL DEFAULT '',
    action        text        NOT NULL CHECK (action ~ '^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$'),
    resource_type text        NOT NULL DEFAULT '',
    resource_id   text        NOT NULL DEFAULT '',
    outcome       text        NOT NULL CHECK (outcome IN ('success', 'failure', 'denied')),
    -- NULL for platform-wide events; set when organisations ship (ADR-0023).
    org_id        text,
    request_id    text        NOT NULL DEFAULT '',
    trace_id      text        NOT NULL DEFAULT '',
    ip            inet,
    user_agent    text        NOT NULL DEFAULT '',
    metadata      jsonb       NOT NULL DEFAULT '{}'
);

-- Listings are newest first by id; each index serves one filter.
CREATE INDEX audit_events_occurred_at ON audit_events (occurred_at);
CREATE INDEX audit_events_action ON audit_events (action, id DESC);
CREATE INDEX audit_events_actor ON audit_events (actor_id, id DESC) WHERE actor_id <> '';
CREATE INDEX audit_events_resource ON audit_events (resource_type, resource_id, id DESC) WHERE resource_type <> '';
CREATE INDEX audit_events_org ON audit_events (org_id, id DESC) WHERE org_id IS NOT NULL;
CREATE INDEX audit_events_request ON audit_events (request_id) WHERE request_id <> '';

-- +goose StatementBegin
CREATE FUNCTION audit_events_reject_update() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'audit events are append-only';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER audit_events_append_only
    BEFORE UPDATE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_reject_update();
