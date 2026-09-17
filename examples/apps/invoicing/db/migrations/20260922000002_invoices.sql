-- Invoices: each invoice belongs to one organisation, whose members reach
-- it through their role. orb gen module wrote this migration; change it
-- freely until it is released, then add a new one.

-- +goose Up
CREATE TABLE invoices (
    id         text        PRIMARY KEY,
    -- The organisation; NOT NULL, so row-level security covers the table.
    org_id     text        NOT NULL,
    -- The member who created it, for display and audit. Access comes only
    -- from membership. Not a foreign key: invoices outlive their creator.
    created_by text        NOT NULL,
    number     text        NOT NULL CHECK (char_length(number) BETWEEN 1 AND 100),
    customer   text        NOT NULL CHECK (char_length(customer) BETWEEN 1 AND 100),
    status     text        NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'sent', 'paid', 'void')),
    note       text        NOT NULL DEFAULT '' CHECK (char_length(note) <= 2000),
    -- Increases with every update; an update must send the version it read.
    version    bigint      NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    -- Other organisation tables reference (org_id, id), so a row can only
    -- point at an invoice of its own organisation.
    UNIQUE (org_id, id)
);

-- Number is unique per organisation, ignoring case.
CREATE UNIQUE INDEX invoices_org_number ON invoices (org_id, lower(number));

-- One index per sort of GET /v1/orgs/{orgId}/invoices.
CREATE INDEX invoices_org_created ON invoices (org_id, created_at, id);
CREATE INDEX invoices_org_updated ON invoices (org_id, updated_at, id);
CREATE INDEX invoices_org_number_sort ON invoices (org_id, (lower(number) COLLATE "C"), id);
CREATE INDEX invoices_org_customer_sort ON invoices (org_id, (lower(customer) COLLATE "C"), id);

-- Purging an organisation deletes its invoices. orgs is the organisations
-- module's table (orgshttp), which the app's migrations run with; an app
-- migrated without it, such as another module's test app, gets the table
-- without the foreign key.
-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass('orgs') IS NOT NULL THEN
        ALTER TABLE invoices ADD CONSTRAINT invoices_org_id_fkey
            FOREIGN KEY (org_id) REFERENCES orgs (id) ON DELETE CASCADE;
    END IF;
END
$$;
-- +goose StatementEnd

-- Row-level security (ADR-0061), as the app's row-level security migration
-- gives every organisation table: a database connection sees and writes only
-- the invoices of the organisation it carries.
ALTER TABLE invoices ENABLE ROW LEVEL SECURITY;
ALTER TABLE invoices FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON invoices
    USING (org_id = current_setting('gorbital.org_id', true) OR current_setting('gorbital.rls_bypass', true) = 'on')
    WITH CHECK (org_id = current_setting('gorbital.org_id', true) OR current_setting('gorbital.rls_bypass', true) = 'on');

-- +goose Down
DROP TABLE invoices;
