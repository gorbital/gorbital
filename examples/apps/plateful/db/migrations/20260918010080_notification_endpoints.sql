-- Notification endpoints: where a restaurant wants to be told that an order
-- arrived or is running late. gorbital has no outbound webhooks and no
-- notification channel but email, so this table and the module above it are
-- the app's own extension of the framework: one Slack-style incoming webhook
-- per row, plus the outcome of the last delivery so the restaurant can see
-- whether its alerts still arrive.

-- +goose Up
-- docs:start notification-endpoints-table
CREATE TABLE notification_endpoints (
    id         text        PRIMARY KEY,
    -- The organisation whose alerts go here. Endpoints are org-scoped: every
    -- query carries the organisation, so a restaurant reaches only its own.
    org_id     text        NOT NULL,
    -- The member who registered it, for display and audit.
    created_by text        NOT NULL,
    -- What the restaurant calls it, such as "Kitchen" or "Front of house".
    label      text        NOT NULL CHECK (char_length(label) BETWEEN 1 AND 60),
    -- The incoming webhook URL, which *is* the secret: anyone holding it can
    -- post into the restaurant's channel, exactly as a password would let
    -- them in. So it is never returned by the API, never logged, never put in
    -- a problem response and never put in an audit event; only the module's
    -- sender ever reads this column, and it reads it one row at a time. A
    -- production deployment would encrypt it at rest (pgcrypto, or an
    -- application-level envelope with a key from a KMS) so that a database
    -- backup is not a bag of live credentials.
    url        text        NOT NULL CHECK (char_length(url) BETWEEN 1 AND 2000),
    -- When the last delivery was attempted, NULL until the first one.
    last_delivery_at timestamptz,
    -- The HTTP status the endpoint answered last, 0 when it never answered
    -- (the connection failed, or nothing has been delivered yet).
    last_status  integer   NOT NULL DEFAULT 0,
    -- Why the last delivery failed, empty when it worked. It names the host
    -- and the status and never the URL, because this column is readable
    -- through the API and the URL is not.
    last_error   text      NOT NULL DEFAULT '' CHECK (char_length(last_error) <= 500),
    -- Increases with every change, including a recorded delivery.
    version    bigint      NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    -- Other organisation tables reference (org_id, id), so a row can only
    -- point at an endpoint of its own organisation.
    UNIQUE (org_id, id)
);
-- docs:end notification-endpoints-table

-- One label per organisation, ignoring case: two endpoints called the same
-- thing would make "delete the broken one" a guess.
CREATE UNIQUE INDEX notification_endpoints_label
    ON notification_endpoints (org_id, lower(label));

-- The fanout job's query: every endpoint of one organisation.
CREATE INDEX notification_endpoints_org ON notification_endpoints (org_id, id);

-- Purging an organisation deletes its endpoints, and with them the webhook
-- URLs it trusted us with. orgs is the organisations module's table
-- (orgshttp), which the app's migrations run with; an app migrated without
-- it, such as another module's test app, gets the table without the foreign
-- key.
-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass('orgs') IS NOT NULL THEN
        ALTER TABLE notification_endpoints ADD CONSTRAINT notification_endpoints_org_id_fkey
            FOREIGN KEY (org_id) REFERENCES orgs (id) ON DELETE CASCADE;
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
DROP TABLE notification_endpoints;
