# ADR-0061: Row-level security option

**Status:** Accepted (2026-09-16) · **Amends:** ADR-0023, ADR-0048, ADR-0050

## Context

v1.1 item 6 (roadmap): multi-tenant apps set the organisation on every database connection, and `orb add rls` adds policies and forces row-level security on org-scoped tables as a fifth isolation layer. ADR-0023 promised it ("Row-level security is an optional additional layer in v1.1"). What exists:

| Area | Today | Evidence |
|---|---|---|
| Isolation | Four layers: `orgs.RequireMember`, repositories taking `orgs.ID`, `org_id NOT NULL` with composite keys, generated cross-organisation tests. A repository query that forgets `org_id` is caught only by tests | ADR-0048 section 8, `start/organisations.md` ("the one layer the compiler can't check") |
| Where the organisation is known | `orgs.RequireMember`/`Authorize` return a context whose `actor.Actor.OrgID` is the checked organisation; the jobs middleware restores `OrgID` from job metadata | `modules/orgs/member.go`, `modules/jobs/middleware.go` |
| Connections | `postgres.Open` builds a pgx pool; repositories hold `DBTX`; transactions through `InTx`; pgx 5.11 has `PrepareConn` (runs on every acquire with the acquiring context) and per-connection `CustomData` | `modules/postgres/postgres.go`, `pgxpool/pool.go` |
| Tables with `org_id NOT NULL` (full-multi) | `projects` (and every `orb gen resource --scope org` table), `org_members`, `org_invitations` | `db/migrations/20260916000001_orgs.sql`, `…_projects.sql` |
| Tables with a nullable `org_id` | `settings_values`, `settings_history`, `jobs_definitions`, `jobs_definition_history`, `audit_events`, `auth_sessions`, `auth_user_roles`, `auth_service_accounts`: platform rows (`NULL`) and organisation rows side by side | library migrations, ADR-0056, ADR-0058 |
| Paths that read org-scoped tables without an organisation actor | Account list of organisations, personal workspace, ownership limits and account deletion (`org_members` by user); accepting an invitation (`org_invitations` by token hash); API key authentication (`auth_service_accounts` by key); settings reload and `/ops/settings/{key}/overrides` (all organisations); `orgs_purge` (`DELETE FROM orgs` cascading); retention and cleanup jobs; migrations; seed and admin commands | `internal/modules/orgs/repository/select_user_orgs.go`, `select_invitation_by_token_hash.go`, ADR-0058, `modules/settings/org.go`, `delete_org.go` |
| Test database | Docker `postgres:18` user `gorbital` is a superuser; superusers and `BYPASSRLS` roles skip every policy, forced or not; a table's owner skips policies unless `FORCE ROW LEVEL SECURITY` | `compose.yaml`, PostgreSQL documentation |
| Golden trees | One tree per preset and tenancy; `gorbital.lock` inputs `preset`, `tenancy`, `mail` select and prove it; `gorbital.yaml` carries `mail` through `SetManifestKey` | ADR-0050, `cli/internal/recipes/release.go` |

Constraints: modules never import each other (ADR-0019), so `modules/orgs` and `modules/settings` can't call `modules/postgres`; released migrations never change; the golden tree stays single (the lead's plan); bypassing policies happens only in code, never from request input, and is logged.

## Options

### How a connection learns its organisation

| | 1. `SET LOCAL` in every transaction | 2. Explicit `postgres.WithOrg(ctx)` in every use case | 3. **Pool hook: `PrepareConn` sets the context's organisation (explicit `WithOrg`, else the actor's `OrgID`) as session settings, remembered per connection** |
|---|---|---|---|
| Covers queries outside transactions | No: most reads run on the pool | Yes | Yes |
| App code changes | Every repository | Every org use case, and every generated one | None: `RequireMember` already puts the organisation on the actor, jobs restore it |
| Forgetting it | Leaks through the missing layer | Same as forgetting `org_id` | Can't forget; a context without an organisation sees no protected rows |
| Cost | A statement per transaction | Same as 3 | One round trip when an acquire changes the settings, none otherwise |
| Verdict | Rejected | Rejected as the main path; kept as an override | **Chosen** |

Setting on acquire and resetting on release (`AfterRelease`) was rejected: pgx runs `AfterRelease` in a goroutine per release and it can't fail a query. Setting on acquire only when the remembered value differs gives the same guarantee (every acquire leaves the connection exactly as its context says) with no cost when nothing changes. A new connection is always set, so a role or database default can't supply either setting.

### Always on, or only with row-level security

| | Hook only after `orb add rls` (option or configuration) | **Hook in every pool from `postgres.Open`** |
|---|---|---|
| Golden tree | A second tree or an environment switch | One tree |
| Rolling out `orb add rls` | Instances started before the migration don't set the organisation and see no rows once it runs | Instances already set it; the migration can run under live traffic |
| Single-tenant apps | — | No actor has an `OrgID`, so connections never change: no statements |
| Verdict | Rejected | **Chosen** |

### Setting names

`gorbital.org_id` and `gorbital.rls_bypass` rather than the plan's `app.org_id` and `app.rls_bypass`: a custom setting needs a prefix, and `app.` is the prefix most hand-written RLS guides use, so an app with its own policies could collide. The names are exported as `postgres.OrgSetting` and `postgres.BypassSetting`.

### Bypass for system paths

| | 1. A second pool and role with `BYPASSRLS` | 2. **`postgres.WithoutRowLevelSecurity(ctx, reason)`: sets `gorbital.rls_bypass = on`; logged once per context through `postgres.WithLogger`, and every query span carries the reason** | 3. Audit event per use |
|---|---|---|---|
| Infrastructure | A second `DATABASE_URL`, secret and pool per app | None | None |
| Strength | A database role boundary | Code boundary: SQL the app runs could set the setting too, as it could `SET ROLE` with option 1's grants | — |
| Noise | — | One log line per bypassing context | An event per migration and job run in `audit_events`, which operators read for people's actions |
| Verdict | Later, for apps whose threat model includes SQL injection into the app's own role | **Chosen**: policies here protect against a missing filter, not a hostile query (queries are placeholders only, ADR-0032) | Rejected |

`postgres.Migrate` uses the bypass (reason `migrate`), so a data migration reaches every organisation. No other golden-app path needs it (below).

### Which tables get policies

| Table | Decision | Why |
|---|---|---|
| `projects`, every `orb gen resource --scope org` table, and any table a developer adds with `org_id NOT NULL` | **Forced row-level security with `org_isolation`** | Tenant data, reached only after `RequireMember` or from a job carrying the organisation |
| `org_members` | Left out | It decides which organisation a request may act in, so `RequireMember` reads it before an organisation is set, and it is read per user across organisations (list of organisations, personal workspace, `orgs.max_owned`, sole-owner checks, account deletion). A policy keyed on the organisation can't protect the table that establishes it; every read would need a bypass on request paths |
| `org_invitations` | Left out | Accepting finds the invitation by the token's hash before its organisation is known; the token is the capability. The alternative, a bypass on a request path, is exactly what the bypass rule forbids |
| Library tables with a nullable `org_id` (settings, audit, jobs, sessions, roles, service accounts) | Left out (no `NOT NULL` column, so never matched) | Platform rows live beside organisation rows and the library reads across organisations (settings reload, `/ops` overrides, API key authentication, retention). A policy allowing platform rows only to system paths would need the bypass inside library modules, which can't import `modules/postgres` (ADR-0019) |
| `orgs` | Left out | The organisation registry has `id`, not `org_id`; it is reached through memberships |

Detecting tables at migration time (a `DO` block over `pg_attribute`) was chosen over parsing migration files in `orb`: it sees tables created by `ALTER TABLE`, by hand and by generated resources, exactly as the database has them.

### Where the policy SQL lives

| | In `cli/internal/recipes/rls/` (like `orgs/convert.sql`) | **`db/row_level_security.sql` in the multi-tenant golden app, copied by `orb add rls`** |
|---|---|---|
| Tested by the app | No: the app can't reach CLI files | Yes: `TestRowLevelSecurity` runs it; `GORBITAL_TEST_RLS=1` runs the whole suite with it |
| Developer edits (another table to leave out) | Lost | Kept: the command copies the app's own file |
| Verdict | Rejected | **Chosen**; not in `db/migrations`, so nothing is applied until `orb add rls` |

### `orb add rls` shape

In place, like `orb add mail`, not on a branch like `orb add orgs`: it writes one new migration and one line in `gorbital.yaml`, merges nothing, and the app needs no code change. A clean git tree is required so the change is reviewable.

## Decision

### Library (`modules/postgres`, additive)

| Addition | Behaviour |
|---|---|
| `OrgSetting` (`gorbital.org_id`), `BypassSetting` (`gorbital.rls_bypass`) | Session settings policies read |
| `Open` | Installs `PrepareConn`: computes the wanted settings from the context (`WithOrg`, else `actor.From(ctx).OrgID`; bypass from `WithoutRowLevelSecurity`); if the connection's remembered settings (pgconn `CustomData`) differ or are unknown, runs `SELECT set_config(…, false), set_config(…, false)` through `PgConn().ExecParams` (one round trip, no statement cache, no span). A failure destroys the connection and fails the query |
| `WithOrg(ctx, orgID)` | Overrides the actor's organisation for connections acquired with ctx |
| `WithoutRowLevelSecurity(ctx, reason)` | Bypass for system paths; panics on a blank reason; logged at info once per context (`row-level security bypassed`, `reason`); spans get `gorbital.rls_bypass` |
| `WithLogger(*slog.Logger)` | Where bypasses are logged. Default: discard |
| `Migrate` | Runs with `WithoutRowLevelSecurity(ctx, "migrate")` |
| `CheckRowLevelSecurity(ctx, db) (RowLevelSecurityReport, error)` | `Role`, `Bypasses` (superuser or `BYPASSRLS`), `Forced`, `NotForced`, `Unprotected` (NOT NULL `org_id` without row-level security); `On()` |

The organisation is taken when a connection is acquired: a transaction keeps its context's organisation from `BeginTx`. Apps must not set the two settings themselves, run `RESET ALL` or `DISCARD ALL`, or put the pool behind a pooler in transaction mode.

### Policy (`db/row_level_security.sql`, full-multi)

For every table in the current schema with a NOT NULL `org_id`, except `org_members` and `org_invitations`: `ENABLE` and `FORCE ROW LEVEL SECURITY`, then `DROP POLICY IF EXISTS` and

```sql
CREATE POLICY org_isolation ON <table>
    USING (org_id = current_setting('gorbital.org_id', true) OR current_setting('gorbital.rls_bypass', true) = 'on')
    WITH CHECK (org_id = current_setting('gorbital.org_id', true) OR current_setting('gorbital.rls_bypass', true) = 'on');
```

Idempotent, so it can run again. `orb gen resource --scope org` in an app with `rls: true` adds the same statements to the resource's migration.

### Paths that need the organisation

| Path | How it is covered |
|---|---|
| Requests to organisation routes | `RequireMember` sets `actor.OrgID`; the hook reads it. `RequireMember`'s own membership read is on `org_members` (left out) |
| Organisation service accounts | Same, after `RequireMember` (ADR-0058); their authentication reads `auth_service_accounts` (left out) |
| Jobs enqueued in an organisation | The jobs middleware restores `OrgID` from metadata; a job that needs another organisation uses `WithOrg` |
| `orgs_purge` | `DELETE FROM orgs` removes rows through `ON DELETE CASCADE`: PostgreSQL runs referential actions as the table owner without forced policies, so no bypass. Verified as a non-superuser owner with `FORCE` |
| `auth_cleanup`, retention, idempotency, rate limit, observability cleanup | Touch no table with a policy |
| Seed | Creates projects through the projects use case, after `RequireMember` |
| Admin commands (`grant-role`, `reset-mfa`, …) | Touch no table with a policy |
| Migrations | `postgres.Migrate` bypasses |
| Code that works across organisations (an app's own maintenance job, export) | `postgres.WithoutRowLevelSecurity(ctx, "job:<name>")` |

### Apps

| App | Change |
|---|---|
| Both Full apps | `postgres.WithLogger(a.logger)` on the pool; `internal/app/rls.go` (identical): at startup, warns when row-level security is on and the role bypasses it, a table isn't forced, or an organisation table has no policy (apart from the two left out); `migrate --status --json` reports the same lines in `row_level_security` |
| full-multi | `db/row_level_security.sql`; tests connect the app as `gorbital_app_test` (no superuser, no `BYPASSRLS`, granted the test database), so policies apply to the suite once they exist; `GORBITAL_TEST_RLS=1` turns them on in every test database; `TestRowLevelSecurity`, `TestRowLevelSecurityCoversOrganisationTables` |

### CLI

| Command | Change |
|---|---|
| `orb add rls` | Refuses apps without a v2 lock, single-tenant and Minimal apps (pointing at `orb add orgs`), apps on an older release (their library doesn't set the organisation) and dirty trees. Copies `db/row_level_security.sql` to `db/migrations/<next>_row_level_security.sql`, sets `rls: true` in `gorbital.yaml` and `inputs.rls` in `gorbital.lock` with the file's new hash, prints next steps (role, migrate, doctor, tests, commit). Already on: nothing changes. `--dry-run`, `--json` (`name`, `already_on`, `migration`, `files`, `dry_run`) |
| `orb gen resource` | `--scope org` in an app whose `gorbital.yaml` says `rls: true` adds the policy to the migration; `--json` gains `row_level_security` |
| `orb upgrade`, `orb add orgs` | Render trees through `inputsTree`, which sets `rls: true` in `gorbital.yaml` when the lock records it, so the base is proven and the line survives; the lock validates that `rls` needs Full and multi-tenant |
| `orb doctor` | A `row-level security` warning per line the app's status reports |

## Why

- Taking the organisation from the actor puts the fifth layer behind the check that already exists on every organisation path, so no use case, template or generated resource changes, and a missing `org_id` filter returns nothing instead of other organisations' rows.
- Always setting it keeps one golden tree and lets the policies arrive by migration under live traffic.
- Remembering the settings per connection makes it free when nothing changes, and destroying a connection whose settings failed means one never carries another organisation's scope.
- Leaving the access-control tables out keeps the bypass off request paths entirely: every request runs under the policies.
- Forcing row-level security means the common setup, an app role that also owns its tables because it runs migrations, is limited too.

## Trade-offs

- Superusers and `BYPASSRLS` roles skip policies. Local Docker databases connect as a superuser, so policies don't apply in `orb dev` unless the developer creates an app role; tests do connect as one. The app warns at startup and `orb doctor` reports it.
- The bypass is a setting any SQL the app runs could change; row-level security here defends against a forgotten filter, not SQL injection. A `BYPASSRLS` system role in a second pool is the stronger, later option.
- One round trip per acquire that changes the organisation. On Docker Desktop that is about 0.37 ms (measured below); a request to an organisation route typically changes it twice (the session lookup without an organisation, then the use case with one). Apps on a network with higher latency pay that latency.
- A list query on a table with the policy costs about 75 µs more in the benchmark (small organisations: the planner estimates the `OR` as selective and sorts a bitmap scan's rows instead of reading the index in order). Organisations with many rows keep the ordered index scan (checked with 300 000 rows in one organisation).
- Session settings don't survive a pooler in transaction mode (PgBouncer `pool_mode=transaction`): another client's transaction could run with a stale organisation. Use session mode or connect directly.
- `org_members` and `org_invitations` keep four layers, not five.
- A policy added to a table by hand after `orb add rls` is the developer's job; startup and `orb doctor` name tables with `org_id NOT NULL` and no policy.
- Changing `db/row_level_security.sql` in a later release doesn't reach apps that already ran `orb add rls`: released migrations never change, so such a change ships as its own migration with an upgrade note.
- Test apps connect as `gorbital_app_test`, which the test server's user must be able to create (Docker PostgreSQL's superuser can).

## Consequences

- ADR-0023: the optional fifth layer exists; the isolation list gains "Database policies (optional)".
- ADR-0048: organisation data can be forced through policies; `org_members` and `org_invitations` stay outside them for the reasons above.
- ADR-0050: `gorbital.lock` inputs gain `rls`; `gorbital.yaml` gains `rls: true`; trees are rendered with it.
- Public API: `modules/postgres` additions (`api/modules-postgres.txt`); CLI command `orb add rls` and its JSON; setting names `gorbital.org_id`, `gorbital.rls_bypass`; policy name `org_isolation`. No error codes, audit actions, permissions, settings or jobs.
- Threat model: new row for cross-organisation reads through a missing filter, mitigated by the option (notes for the lead).
- Guide: [Row-level security](../guides/row-level-security.md).

## Implementation notes (2026-09-16)

- An experiment on PostgreSQL 18 confirmed: a non-superuser owner with `FORCE` sees only its organisation; `DELETE FROM orgs` cascades to forced tables without a bypass; `options=-c role=<role>` in a connection URL makes a superuser's session subject to policies (used by tests).
- `pgx` reads `+` in a URL query literally, so test URLs encode the space in `-c role=…` as `%20`.

| Check | Result |
|---|---|
| `modules/postgres` `TestRowLevelSecurityFollowsTheContext` (one connection reused across 10 steps: none, actor, none again, another organisation, `WithOrg` over the actor, system actor, bypass twice, after bypass, unknown organisation; settings values; transaction; `WITH CHECK` refuses an insert into another organisation, an update of its row changes nothing; cascade without bypass; one log line; bypass spans only on bypassed queries) | pass |
| `TestRowLevelSecuritySettingFailureDropsTheConnection` (a setting PostgreSQL refuses fails the query, the connection is replaced, the next context sees nothing) | pass |
| `TestMigrateBypassesRowLevelSecurity` (a data migration updates every organisation's rows as a non-superuser owner; the connections return without the bypass) | pass |
| `TestCheckRowLevelSecurity` (forced, not forced, unprotected, bypassing superuser, empty database), `TestWithoutRowLevelSecurityNeedsAReason`, `TestOpenRejectsNilLogger` | pass |
| Mutation: never resetting a connection to "no organisation" | `TestRowLevelSecurityFollowsTheContext` and `TestMigrateBypassesRowLevelSecurity` fail |
| `BenchmarkRowLevelSecurity` (Apple M1 Max, Docker Desktop PostgreSQL 18, one connection) | same organisation 0.39 ms/op (a query's round trip); switching organisation every acquire 0.77 ms/op (+1 round trip); list query with `org_id` filter on 20 000 rows in 200 organisations: 0.41 ms without the policy, 0.49 ms with it |
| full-multi `TestRowLevelSecurity` (policies turned on under a running app: create, list, update, delete over HTTP; 404 across organisations; a query without `org_id` sees 1, 1, 0 and, bypassed, 2 projects; forged insert refused; seed and migrate as the app's role; `orgs_purge` removes a deleted organisation's projects; `auth_cleanup` runs; status as the app's role reports nothing, as a superuser the bypass) | pass |
| Mutation: the hook ignoring the actor's organisation | `TestRowLevelSecurity` fails (creating a project: 500) |
| full-multi `TestRowLevelSecurityCoversOrganisationTables` (projects forced; only `org_invitations` and `org_members` left; one policy after running twice; nothing reported without it) | pass |
| full-multi whole `internal/app` suite with `GORBITAL_TEST_RLS=1` (every test app with policies, as `gorbital_app_test`) | pass |
| Both Full apps: gofmt, vet, `go test ./...` (full-multi tests now as `gorbital_app_test`) | pass |
| cli `TestAddRLSWritesTheMigration` (dry run writes nothing; migration after every other, equal to the app's file; `rls: true` in both files; lock rebuilds; second run no-op; `orb upgrade` up to date; generated organisation resource has the policy, user resource doesn't) | pass |
| Mutation: `inputsTree` ignoring `rls` | lock no longer rebuilds and `orb upgrade` conflicts on `gorbital.yaml` |
| cli `TestAddRLSRefuses` (single-tenant, Minimal, older release, dirty tree, lock or manifest claiming `rls` for a single-tenant app), `TestDoctorReportsRowLevelSecurity`, `TestJSONOutputs` (`add-rls.json` recorded) | pass |
| `ORB_E2E=1` cli `TestAddRLS` (77 s): a new multi-tenant app's database with projects in two organisations; `orb add rls`, migrate; only `projects` forced, one policy; as a role without bypass a query without `org_id` sees 2, 1 and 0 projects for the two organisations and none; `orb doctor` warns about the superuser in `.env`; a generated `Customer` resource's migration adds the second policy; the app's whole suite, the generated resource and `TestRowLevelSecurity` included, passes with the policies in every test database | pass (first run found `TestRowLevelSecurityCoversOrganisationTables` assuming no policies in a fresh database, which an app after `orb add rls` has; fixed) |
| `go run -C internal/tools/apicheck .` | additions only, recorded |
| golangci-lint | not installed locally; not run |
