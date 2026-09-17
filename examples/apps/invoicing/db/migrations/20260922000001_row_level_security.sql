-- Row-level security (ADR-0061): the database refuses one company's invoices
-- to a request acting in another, even if a query forgets its org_id filter.
-- It is what orb add rls writes in a multi-tenant v0.1 app: the DO block of
-- examples/full-multi/db/row_level_security.sql under a version of this app.
-- orb add rls needs a v0.1 app's gorbital.lock, so here it was added by hand.
--
-- The block gives every table with a NOT NULL org_id column that exists when
-- it runs row-level security, forced so the table's owner is limited too, and
-- one policy: a connection sees and writes only rows of the organisation it
-- carries (gorbital.org_id, which guard.OrgMember sets through
-- postgres.WithOrg), unless code bypassed the policies for a system path
-- (gorbital.rls_bypass; only migrations do).
--
-- It runs before the invoices migration, so it protects no app table yet: the
-- file's name is what orb gen module --org looks for, and the invoices
-- migration it wrote afterwards carries the same policy itself. Tables added
-- by hand later need the statements in their own migration.
--
-- Left out, on purpose: org_members and org_invitations, the organisations
-- module's own, which are read before an organisation is known.
--
-- The app's database role must not be a superuser or have BYPASSRLS:
-- PostgreSQL applies no policy to those. The app warns at startup.

-- +goose Up
-- docs:start row-level-security
-- +goose StatementBegin
DO $$
DECLARE
    t regclass;
BEGIN
    FOR t IN
        SELECT c.oid::regclass
        FROM pg_class c
        JOIN pg_attribute a ON a.attrelid = c.oid AND a.attname = 'org_id' AND a.attnotnull AND NOT a.attisdropped
        WHERE c.relnamespace = current_schema()::regnamespace
          AND c.relkind IN ('r', 'p')
          AND c.relname NOT IN ('org_members', 'org_invitations')
        ORDER BY c.relname
    LOOP
        EXECUTE format('ALTER TABLE %s ENABLE ROW LEVEL SECURITY', t);
        EXECUTE format('ALTER TABLE %s FORCE ROW LEVEL SECURITY', t);
        EXECUTE format('DROP POLICY IF EXISTS org_isolation ON %s', t);
        EXECUTE format($policy$
            CREATE POLICY org_isolation ON %s
                USING (org_id = current_setting('gorbital.org_id', true) OR current_setting('gorbital.rls_bypass', true) = 'on')
                WITH CHECK (org_id = current_setting('gorbital.org_id', true) OR current_setting('gorbital.rls_bypass', true) = 'on')
        $policy$, t);
    END LOOP;
END
$$;
-- +goose StatementEnd
-- docs:end row-level-security
