-- Organisations for an app that started single-tenant (ADR-0048, ADR-0050),
-- written by aps add orgs to run after the organisations migration: every
-- account gets a personal workspace, and each project moves into its
-- owner's workspace. Projects are changed in place, so columns you added
-- to the table are kept.

-- +goose Up
-- +goose StatementBegin
-- An organisation ID as the app makes them (orgs.NewID): org_ and 128
-- random bits in unpadded lowercase base32. The bits come from
-- gen_random_uuid, skipping each UUID's fixed version and variant bits.
CREATE FUNCTION pg_temp.aps_new_org_id() RETURNS text
LANGUAGE sql VOLATILE AS $$
    WITH r AS (
        SELECT substring(uuid_send(gen_random_uuid()) FROM 1 FOR 6)
            || substring(uuid_send(gen_random_uuid()) FROM 10 FOR 7)
            || substring(uuid_send(gen_random_uuid()) FROM 1 FOR 3) AS b
    )
    SELECT 'org_' || string_agg(substr('abcdefghijklmnopqrstuvwxyz234567', c.v + 1, 1), '' ORDER BY i)
    FROM r, generate_series(0, 25) AS i,
    LATERAL (
        -- Five bits, most significant first; get_bit counts from the right
        -- of each byte. The last two bits of the 130 are zero.
        SELECT sum(CASE WHEN k < 128 AND get_bit(r.b, (k / 8) * 8 + 7 - k % 8) = 1
                        THEN 1 << (4 - (k - i * 5)) ELSE 0 END)::int AS v
        FROM generate_series(i * 5, i * 5 + 4) AS k
    ) AS c
$$;
-- +goose StatementEnd

-- A personal workspace for every account without one. A deleted account's
-- workspace is deleted too, and purged when the account would have been
-- (the 30-day default of auth.deleted_account_retention).
INSERT INTO orgs (id, name, personal, created_by, version, created_at, updated_at, deleted_at, purge_after)
SELECT pg_temp.aps_new_org_id(), 'Personal', true, u.id, 1, now(), now(), u.deleted_at, u.deleted_at + interval '30 days'
FROM auth_users u
WHERE NOT EXISTS (SELECT 1 FROM orgs o WHERE o.created_by = u.id AND o.personal);

INSERT INTO org_members (org_id, user_id, role, joined_at, added_by)
SELECT o.id, o.created_by, 'owner', o.created_at, 'system:orgs'
FROM orgs o
JOIN auth_users u ON u.id = o.created_by
WHERE o.personal
ON CONFLICT DO NOTHING;

-- +goose StatementBegin
-- Projects belong to organisations: org_id replaces owner_id, and the owner
-- is kept as created_by. Skipped when the example projects table was
-- removed or already changed.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema() AND table_name = 'projects' AND column_name = 'owner_id'
    ) THEN
        RETURN;
    END IF;

    ALTER TABLE projects ADD COLUMN org_id text REFERENCES orgs (id) ON DELETE CASCADE;
    ALTER TABLE projects ADD COLUMN created_by text;
    UPDATE projects p SET org_id = o.id, created_by = p.owner_id
    FROM orgs o
    WHERE o.personal AND o.created_by = p.owner_id;
    ALTER TABLE projects ALTER COLUMN org_id SET NOT NULL;
    ALTER TABLE projects ALTER COLUMN created_by SET NOT NULL;
    ALTER TABLE projects ADD UNIQUE (org_id, id);

    DROP INDEX projects_owner_name;
    DROP INDEX projects_owner_created;
    DROP INDEX projects_owner_updated;
    DROP INDEX projects_owner_name_sort;
    ALTER TABLE projects DROP COLUMN owner_id;

    CREATE UNIQUE INDEX projects_org_name ON projects (org_id, lower(name));
    CREATE INDEX projects_org_created ON projects (org_id, created_at, id);
    CREATE INDEX projects_org_updated ON projects (org_id, updated_at, id);
    CREATE INDEX projects_org_name_sort ON projects (org_id, (lower(name) COLLATE "C"), id);
END
$$;
-- +goose StatementEnd

DROP FUNCTION pg_temp.aps_new_org_id();
