-- Row-level security (ADR-0061): a fifth isolation layer for organisation
-- data, on top of guard.OrgMember, org-scoped repositories, composite keys
-- and the cross-organisation tests. orb add rls copies this file into
-- db/migrations under a new version; until then nothing here is applied.
--
-- Every table in the schema with a NOT NULL org_id column gets row-level
-- security, forced so the table's owner is limited too, and one policy: a
-- connection sees and writes only rows of the organisation it carries
-- (gorbital.org_id, set by modules/postgres from the request's organisation),
-- unless code bypassed the policies for a system path (gorbital.rls_bypass).
-- Tables created later get the policy from orb gen module --org, or by hand.
--
-- Left out, on purpose:
--   org_members      decides which organisation a request may act in, so it
--                    is read before one is known, and per user across
--                    organisations (their list, ownership limits, account
--                    deletion)
--   org_invitations  found by the token in the link before its organisation
--                    is known
-- Library tables with a nullable org_id (settings, audit events, jobs,
-- sessions, roles, service accounts) hold platform rows too and are read
-- across organisations by the library, so they have no NOT NULL org_id.
--
-- The app's database role must not be a superuser or have BYPASSRLS:
-- PostgreSQL applies no policy to those. The app warns at startup, and orb
-- doctor reports it.

-- +goose Up
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
