-- Incidents (ADR-0064): opened by operators or by error-rate detection, with
-- a timeline of updates.

-- +goose Up
CREATE TABLE incidents (
    id              bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title           text        NOT NULL,
    -- Written by operators: can hold personal data they chose to write.
    summary         text        NOT NULL DEFAULT '',
    severity        text        NOT NULL CHECK (severity IN ('sev1', 'sev2', 'sev3', 'sev4')),
    status          text        NOT NULL CHECK (status IN ('investigating', 'identified', 'monitoring', 'resolved')),
    source          text        NOT NULL CHECK (source IN ('manual', 'automatic')),
    started_at      timestamptz NOT NULL,
    resolved_at     timestamptz,
    -- Automatic incidents: when detection last saw the error rate recover;
    -- NULL while it is high.
    recovered_at    timestamptz,
    created_by_kind text        NOT NULL,
    created_by_id   text        NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT statement_timestamp(),
    updated_at      timestamptz NOT NULL DEFAULT statement_timestamp(),
    CHECK ((status = 'resolved') = (resolved_at IS NOT NULL))
);

-- At most one open automatic incident, whichever instance detects it.
CREATE UNIQUE INDEX incidents_one_open_automatic ON incidents (source) WHERE source = 'automatic' AND status <> 'resolved';
CREATE INDEX incidents_started_at ON incidents (started_at);

CREATE TABLE incident_updates (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    incident_id bigint      NOT NULL REFERENCES incidents (id) ON DELETE CASCADE,
    kind        text        NOT NULL CHECK (kind IN ('opened', 'update', 'resolved', 'recovered', 'breaching')),
    message     text        NOT NULL,
    -- The incident's status and severity after the update.
    status      text        NOT NULL,
    severity    text        NOT NULL,
    actor_kind  text        NOT NULL,
    actor_id    text        NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT statement_timestamp()
);

CREATE INDEX incident_updates_incident_id ON incident_updates (incident_id, id);
