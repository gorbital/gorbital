# Row-level security

In a multi-tenant app, four layers keep organisations apart: `orgs.RequireMember`, repositories that take the organisation ID, `org_id` keys in the database, and cross-organisation tests. One thing none of them catches at run time is a query that forgets `WHERE org_id = $1`. Row-level security adds a fifth layer in PostgreSQL itself: with it on, a query that forgets its filter sees only the rows of the organisation its connection carries, and a write can't put a row in another organisation. Decision: [ADR-0061](../adr/0061-row-level-security.md).

It is optional. Turn it on with one command:

```bash
orb add rls
go run ./cmd/migrate
```

## In an app on gorbital.Main

Everything below holds in an app built with `gorbital.Main` and [`orgshttp`](../start/organisations.md#in-an-app-on-gorbitalmain), with two differences:

- **The organisation comes from the guard.** [`guard.OrgMember`](../methods/gorbital-guard.md#OrgMember) sets the actor's organisation and `postgres.WithOrg` before the handler runs, so every query the handler and its use cases run carries the organisation of the path they were authorized for. The organisations module's own use cases set it through `orgs.RequireMember`, as in v0.1.
- **The policies are a migration you add.** `orb add rls` needs the `gorbital.lock` of an app created with `orb new --tenancy multi`, which apps on `gorbital.Main` don't have yet. Add a migration to `db/migrations`, under a version after your latest one, with the `DO` block a multi-tenant v0.1 app keeps in `db/row_level_security.sql` (the [invoicing recipe](../examples/recipes/multi-tenant-invoicing.md) shows it); it protects every table with `org_id NOT NULL` except `org_members` and `org_invitations`, the organisations module's own. Modules generated afterwards with `orb gen module --org` carry the policy in their own migration.

`gorbital.New` logs the same warnings at start as a v0.1 app (a role that bypasses the policies, tables not forced, organisation tables without a policy), and `migrate --status` works as `cmd/migrate --status` did.

Only one path in the library bypasses the policies: `postgres.Migrate`, for migrations. Requests, `guard.OrgMember`, the organisations module and the `orgs_purge` job never do; the purge removes an organisation's rows through `ON DELETE CASCADE`. A test lists every call to `postgres.WithoutRowLevelSecurity` in the repository, so a new one needs a review ([ADR-0083](../adr/0083-modules-stack-migrations-and-ejection.md#threat-model-phase-7)).

## How it works

<div class="steps">

1. **Every connection carries its organisation**

   `postgres.Open` gives every pool a hook that runs when a connection is taken from the pool. It reads the organisation from the context: the actor's `OrgID`, which `orgs.RequireMember` sets after checking membership, or an explicit `postgres.WithOrg(ctx, orgID)`. It sets two session settings on the connection, `gorbital.org_id` and `gorbital.rls_bypass`, and remembers them, so a connection that already has the right values costs nothing. Every multi-tenant app does this from creation, whether or not the policies exist.

2. **Policies read it**

   `orb add rls` copies `db/row_level_security.sql` into a new migration. For every table with a `NOT NULL org_id` column (memberships and invitations aside, see below) it turns row-level security on, forces it so the table's owner is limited too, and adds one policy:

   ```sql
   CREATE POLICY org_isolation ON projects
       USING (org_id = current_setting('gorbital.org_id', true) OR current_setting('gorbital.rls_bypass', true) = 'on')
       WITH CHECK (org_id = current_setting('gorbital.org_id', true) OR current_setting('gorbital.rls_bypass', true) = 'on');
   ```

3. **New tables get it too**

   After `orb add rls`, `gorbital.yaml` says `rls: true`, and `orb gen resource --scope org` adds the same statements to the new resource's migration. A table you write by hand needs them in its migration; the app warns at startup about organisation tables without a policy.

</div>

A context without an organisation (a sign-in, a platform job) sees no rows of protected tables and can't write to them.

## The database role

> [!WARNING]
> PostgreSQL applies no policy to a superuser or to a role with `BYPASSRLS`, forced or not. The app's `DATABASE_URL` must connect as a role with neither.

The local Docker database from `orb dev` connects as its superuser, so policies don't apply there. To try them locally, create a role for the app:

```sql
CREATE ROLE acme_app LOGIN PASSWORD 'change-me' NOSUPERUSER NOBYPASSRLS;
GRANT USAGE, CREATE ON SCHEMA public TO acme_app;
GRANT SELECT, INSERT, UPDATE, DELETE, TRUNCATE ON ALL TABLES IN SCHEMA public TO acme_app;
GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA public TO acme_app;
```

In production, the role that runs migrations usually owns the tables. That is fine: the policies are forced, so the owner is limited too. What must not happen is connecting as a superuser, or as a managed database's administrator role when it has `BYPASSRLS`.

The app checks at startup and logs a warning when row-level security is on and:

- the role is a superuser or has `BYPASSRLS`,
- a table has row-level security on but not forced,
- a table with `org_id NOT NULL` has no row-level security.

`orb doctor` reports the same, through `go run ./cmd/migrate --status`.

## Code that works across organisations

Migrations already run across organisations: `postgres.Migrate` bypasses the policies, so a data migration reaches every row. For your own system paths, such as a maintenance job or an export, bypass them explicitly:

```go internal/jobs/reindex/reindex.go
ctx = postgres.WithoutRowLevelSecurity(ctx, "job:reindex")
rows, err := pool.Query(ctx, `SELECT id, org_id FROM invoices WHERE indexed_at IS NULL LIMIT 500`)
```

- The reason names the path. The first connection taken with that context is logged at info level (`row-level security bypassed`, `reason=job:reindex`), and every query span it runs carries `gorbital.rls_bypass`.
- Decide it in code. Never bypass because of anything in a request.
- To act in one organisation without an organisation actor, use `postgres.WithOrg(ctx, orgID)` instead.

Jobs enqueued inside an organisation's request carry its organisation in their metadata, and the jobs middleware puts it back on the actor, so they need neither.

The organisation purge needs no bypass: deleting a row from `orgs` removes its projects through `ON DELETE CASCADE`, and PostgreSQL runs those referential actions without applying policies.

## What is left out, and why

| Table | Why it has no policy |
|---|---|
| `org_members` | It decides which organisation a request may act in, so it is read before one is known, and per user across organisations: the list of your organisations, ownership limits, account deletion |
| `org_invitations` | Accepting finds the invitation by the token in the link, before its organisation is known |
| Library tables with a nullable `org_id` (settings, audit events, jobs, sessions, roles, service accounts) | They hold platform rows beside organisation rows, and the library reads across organisations |
| `orgs` | The organisation registry itself |

Every query on these tables is written by gorbital and covered by cross-organisation tests. To leave out one of your own tables, add it to the `NOT IN` list in `db/row_level_security.sql` before running `orb add rls`, and to `rlsLeftOut` in `internal/app/rls.go`.

## Testing

Multi-tenant apps' tests connect the app as `gorbital_app_test`, a role without `BYPASSRLS` that the tests create and grant, so once `orb add rls` has run, every test runs under the policies. `TestRowLevelSecurity` checks requests, seed data, migrations and the purge with the policies on, and that a query without `org_id` sees only one organisation.

Before running `orb add rls`, you can run the whole suite as it would be after:

```bash
GORBITAL_TEST_RLS=1 go test ./internal/app
```

The test server's user must be able to create roles, as Docker PostgreSQL's superuser can.

## Cost

Measured on an Apple M1 Max with PostgreSQL 18 in Docker Desktop ([ADR-0061](../adr/0061-row-level-security.md#implementation-notes-2026-09-16)):

| | |
|---|---|
| Taking a connection that already carries the organisation | Nothing sent |
| Taking one that carries another organisation, or none | One round trip (0.37 ms there). A request to an organisation route usually changes it twice: the session lookup has no organisation, the use case has one |
| A list query with its `org_id` filter, with the policy | About 75 µs more with small organisations; large organisations keep an ordered index scan |

## Limits

- Session settings don't survive a connection pooler in transaction mode (PgBouncer `pool_mode=transaction`): a connection could reach another client with a stale organisation. Use session mode, or connect directly.
- Don't set `gorbital.org_id` or `gorbital.rls_bypass` yourself, and don't run `RESET ALL` or `DISCARD ALL`.
- The organisation is taken when a connection is taken: a transaction keeps the organisation of the context that began it.
- Row-level security protects against a missing filter, not against SQL injection: SQL running as the app's role could change the settings. Queries use placeholders only.
- Changes to `db/row_level_security.sql` in a later release don't reach apps that already ran `orb add rls`; such a change comes as its own migration with an upgrade note.
