# Multi-tenant invoicing

A billing API that many companies share. Each company is an **organisation**: its owner invites colleagues by email, and its members create and send the company's invoices. No other company can see them, and that holds in four places: the route guard, every SQL statement, the table's keys, and PostgreSQL's row-level security, which keeps working even if a query forgets its `org_id` filter.

The app is in `examples/apps/invoicing/`. It has one module of its own, `invoices`, which `orb gen module --org` wrote and nobody edited since. Everything else is the library's.

| Route | Who | Does |
|---|---|---|
| `/v1/auth/*` | Anyone | Registration, email verification, sign-in, sessions, API keys (`authhttp`) |
| `GET /v1/orgs`, `POST /v1/orgs` | Signed-in users | Lists the caller's companies, personal workspace first; creates a company, owned by the caller |
| `POST /v1/orgs/{orgId}/invitations` | The company's owners and admins | Emails an invitation with a role |
| `POST /v1/invitations/accept` | The invited account | Joins the company with the token from the email |
| `/v1/orgs/{orgId}/members` | The company's members | Lists members; owners and admins change roles and remove members |
| `POST`, `GET /v1/orgs/{orgId}/invoices` | The company's members (`invoices.invoice.write`, `.read`) | Creates an invoice; lists them, by `created_at`, `updated_at`, `number` or `customer`, filtered by `status` |
| `GET`, `PATCH`, `DELETE /v1/orgs/{orgId}/invoices/{id}` | The company's members | Reads, updates (with the `version` read) or deletes an invoice |
| `/ops/*` | Operators (`platform_admin`, `ops_viewer`) | Settings, retention, rate limits, jobs, audit log |

## main.go

<!-- include examples/apps/invoicing/cmd/api/main.go#main -->

| Module | Adds |
|---|---|
| `authhttp.New()` | Sign-in. Organisation members are its accounts, so `orgshttp.Module` takes it |
| `opshttp.Module()` | The operations API under `/ops/` |
| `orgshttp.Module(auth)` | Organisations, members, invitations, organisations' service accounts and settings, the `orgs_purge` job, and the `orgs` tables' migrations |
| `modules.All()` | The app's own modules: here, `invoices` |
| `migrations.FS` | The row-level security migration and the invoices table |

`gorbital.New` refuses to start when `auth` isn't the authenticator given to `WithAuth`, and when a route uses `guard.OrgMember` without `orgshttp`. Details: [Organisations in an app on gorbital.Main](../../start/organisations.md#in-an-app-on-gorbitalmain), [orgshttp](../../methods/gorbital-orgshttp.md).

## Companies and invitations

Every account gets a personal workspace when it registers. A company is an organisation of its own, created by someone with a verified email address, who becomes its `owner`:

```bash
curl -X POST http://127.0.0.1:8080/v1/orgs \
  -H "Authorization: Bearer $ADA" -H 'Content-Type: application/json' \
  -d '{"name":"Acme Ltd"}'
```

```text
201 {"id":"org_…","name":"Acme Ltd","personal":false,"role":"owner","version":1,…}
```

Ada invites Bob, who works in Acme's accounts team:

```bash
curl -X POST http://127.0.0.1:8080/v1/orgs/$ACME/invitations \
  -H "Authorization: Bearer $ADA" -H 'Content-Type: application/json' \
  -d '{"email":"bob@acme.example","role":"member"}'
```

The email links to the page in the `orgs.invitation_url` runtime setting, with a single-use token in the URL fragment. Bob registers or signs in with that address, and the page posts the token:

```bash
curl -X POST http://127.0.0.1:8080/v1/invitations/accept \
  -H "Authorization: Bearer $BOB" -H 'Content-Type: application/json' \
  -d '{"token":"…"}'
```

```text
200 {"id":"org_…","name":"Acme Ltd","personal":false,"role":"member",…}
```

| Role | In Acme |
|---|---|
| `owner` | Everything, including deleting the company and managing owners |
| `admin` | Renames the company, changes its settings, invites and manages members and admins |
| `member` | Sees the company and its members |

All three roles work with invoices (next section). Only the invited address can accept, invitations expire, and resending replaces the token. More: [Roles](../../start/organisations.md#roles), [Invitations](../../start/organisations.md#invitations).

## Generating the invoices module

An invoice has a number, unique in its company, a customer, a status and a note:

```bash
orb gen module Invoice number:string:unique customer:string 'status:enum(draft,sent,paid,void)' note:text --org
```

```text
✓ Created module invoices

  Module:      invoices (table invoices, IDs like inv_…)
  API:         /v1/orgs/{orgId}/invoices, for an organisation's invoices (guard.OrgMember)
  Permissions: invoices.invoice.read, invoices.invoice.write (organisation roles owner, admin and member)
  Security:    row-level security policy in the migration
  Fields:
    number               string, 1 to 100 characters, unique
    customer             string, 1 to 100 characters
    status               one of draft, sent, paid, void (default draft)
    note                 text, up to 2000 characters
  Files:
    create internal/modules/invoices/module.go
    create internal/modules/invoices/invoices_test.go
    …
    create db/migrations/20260922000002_invoices.sql
    create internal/modules/architecture_test.go
    modify internal/modules/modules.gen.go
```

`Security: row-level security policy in the migration` is there because the app already had its row-level security migration when the command ran ([below](#row-level-security)). The field types are the generator's: strings, text and enums. An amount in cents or line items are columns and code you add yourself, in a new migration (`orb gen migration`) and the four layers ([Generating code](../../guides/generating-code.md#adding-an-operation-by-hand)).

### Permissions

<!-- include examples/apps/invoicing/internal/modules/invoices/module.go -->

The permissions name `OrgRoles`, not `Roles`: they are organisation permissions, which a member holds through their role in the company the request acts in. Platform roles grant nothing inside a company, and an API key only what its scopes include. To let members read invoices but only owners and admins write them, drop `"member"` from `PermWrite`. A role name of the app's own, such as `billing`, only needs naming in `OrgRoles` to become an organisation role that owners and admins can give.

### The route table

<!-- include examples/apps/invoicing/internal/modules/invoices/delivery/routes.go -->

Every route has [`guard.OrgMember`](../../methods/gorbital-guard.md#OrgMember) with the permission it needs. Before the body is read, it asks the organisations module about the `{orgId}` in the path:

| Caller | Answer |
|---|---|
| Not signed in | 401 `unauthenticated` |
| Not a member of the company, or the company is unknown, deleted, or its ID malformed | 404 `org_not_found`, the same for all four, so company IDs can't be probed |
| A member whose role lacks the permission, or an API key whose scopes lack it | 403 `forbidden` |
| A member whose role needs a second factor the session hasn't verified | 403 `mfa_required` |
| A member whose role grants it | The handler runs with an actor acting in the company: its `OrgID`, the role's permissions, audit events recording the company, and database connections carrying it (`postgres.WithOrg`) |

### Every statement filters on the company

The guard decides who may act in a company; the SQL decides which rows are the company's. Every statement the module wrote takes the organisation from the path:

<!-- include examples/apps/invoicing/internal/modules/invoices/repository/select_invoice.go -->

So a member of Globex who sends one of Acme's invoice IDs under Globex's path gets 404 `invoice_not_found`, row-level security or not.

## Row-level security

Row-level security makes PostgreSQL itself return and accept only the rows of the organisation a connection carries ([Row-level security](../../guides/row-level-security.md#in-an-app-on-gorbitalmain)). `guard.OrgMember` already sets that organisation on the request's connections; the policies are migrations.

`orb add rls` writes them in apps created with `orb new --tenancy multi`, from their `gorbital.lock`, which an app on `gorbital.Main` doesn't have. Here the same migration was added by hand, as `db/migrations/20260922000001_row_level_security.sql`, with the `DO` block of a multi-tenant app's `db/row_level_security.sql`:

<!-- include examples/apps/invoicing/db/migrations/20260922000001_row_level_security.sql#row-level-security -->

For every table with an `org_id NOT NULL` column that exists when it runs, apart from the organisations module's `org_members` and `org_invitations`, it turns row-level security on, forces it so the table's owner is limited too, and adds the `org_isolation` policy.

### Order: the policy migration first, then the module

The block covers the tables that exist when it runs, so there are two ways to order it:

| Order | Result |
|---|---|
| Module first, then the row-level security migration | The block covers `invoices`. Every table generated later needs the policy in its own migration |
| **Row-level security migration first, then `orb gen module --org`** (this app) | The block covers nothing of the app's yet. `orb gen module --org` finds a `db/migrations/*_row_level_security.sql` file and ends the table's migration with the same statements |

This app does the second, so both halves are visible and every module generated from now on is covered the same way. The invoices migration, as generated:

<!-- include examples/apps/invoicing/db/migrations/20260922000002_invoices.sql -->

Both write the same policy, and the block drops a policy of that name before creating it, so running it again later (in a new migration) covers new hand-written tables without conflicting with generated ones.

### The database role

> [!WARNING]
> PostgreSQL applies no policy to a superuser or to a role with `BYPASSRLS`, forced or not. Production's `DATABASE_URL` must connect as a role with neither. The app logs a warning at startup when it doesn't.

The compose database's user is a superuser, so locally the policies don't apply and the app warns:

```text
level=WARN msg="row-level security is on, but the database role invoicing is a superuser or has BYPASSRLS, so no policy applies to it; connect as a role without either (docs/guides/row-level-security.md)"
```

To see them apply, migrate as the compose user, then create a role for the app and run it with that role:

```bash
go run ./cmd/api migrate
docker compose exec -T postgres psql -U invoicing -d invoicing <<'SQL'
CREATE ROLE invoicing_app LOGIN PASSWORD 'change-me' NOSUPERUSER NOBYPASSRLS;
GRANT USAGE ON SCHEMA public TO invoicing_app;
GRANT SELECT, INSERT, UPDATE, DELETE, TRUNCATE ON ALL TABLES IN SCHEMA public TO invoicing_app;
GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA public TO invoicing_app;
SQL
```

<!-- include examples/apps/invoicing/.env.example#app-role -->

The warning is gone, and every request's queries on `invoices` see one company. Grant again after a migration adds tables. Code that must work across companies, such as a maintenance job, says so with `postgres.WithoutRowLevelSecurity(ctx, "job:…")`; migrations already do ([Code that works across organisations](../../guides/row-level-security.md#code-that-works-across-organisations)).

## Tests

Run them with PostgreSQL up (`orb dev`, or `docker compose up -d --wait`):

```bash
export GORBITAL_TEST_DATABASE_URL='postgres://invoicing:invoicing@127.0.0.1:5432/invoicing?sslmode=disable'
export GORBITAL_REQUIRE_DB=1
go test ./...
```

`cmd/api/invoicing_test.go` builds the app with `main.go`'s options and signs people up with [`gorbitaltest.App.SignUp`](../../methods/gorbital-gorbitaltest.md#App.SignUp), which registers, verifies the address from the queued email and signs in: organisation members must be real accounts. Two companies, an invitation, and each company's invoices out of the other's reach:

<!-- include examples/apps/invoicing/cmd/api/invoicing_test.go#companies -->

The HTTP tests connect as the test server's superuser, so they check the guard and the SQL, not the policies. The row-level security test connects as a role without superuser or `BYPASSRLS`, created on the test server, and runs SQL without an `org_id` filter:

<!-- include examples/apps/invoicing/cmd/api/invoicing_test.go#row-level-security -->

| Test | Checks |
|---|---|
| `TestCompanies` | Invitations by email; `org_not_found` for another company's routes; `invoice_not_found` for its invoice under your own path; numbers unique per company |
| `TestRowLevelSecurity` | Acting in Acme, a query without a filter sees Acme's rows only, and none without an organisation; an insert into Globex is refused with `42501`; a superuser sees everything |
| `internal/modules/invoices/invoices_test.go` (generated) | Create and get, the rules, deny by default, another user on every route, another organisation's invoice, a read-only API key, pages, versions, the audit trail with the organisation, purging an organisation deleting its invoices |
| `TestOpenAPIIsCurrent` (`cmd/api/main_test.go`) | `api/openapi.json` matches the code; after changing a route, run `go run ./cmd/api openapi --dir api` |
| `internal/modules/architecture_test.go` (generated) | The layers import only what they may |

More: [Testing with gorbitaltest](../../guides/testing-with-gorbitaltest.md), [Row-level security: testing](../../guides/row-level-security.md#testing).

## Next

- [Organisations](../../start/organisations.md): roles, personal workspaces, invitations, deletion and organisation settings.
- [Row-level security](../../guides/row-level-security.md): how the policies work, the database role, what is left out, cost and limits.
- [Shelfie, 8. Book clubs](../shelfie/08-book-clubs.md): the same features in a larger app, step by step.
- Methods: [orgshttp](../../methods/gorbital-orgshttp.md), [guard.OrgMember](../../methods/gorbital-guard.md#OrgMember), [postgres.WithOrg](../../methods/modules-postgres.md#WithOrg), [gorbitaltest](../../methods/gorbital-gorbitaltest.md).
