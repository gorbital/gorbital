# Invoicing

A multi-tenant invoicing API: every company is an organisation, its owner
invites colleagues by email, and members keep the company's invoices, which
nobody outside the company can see, down to the database's row-level
security. It is the app of the recipe *Multi-tenant invoicing* in gorbital's
Examples tab (`docs/examples/recipes/multi-tenant-invoicing.md`); every code
block on that page is included from this directory.

```text
cmd/api/main.go                                  gorbital.Main with sign-in, /ops, organisations and the app's modules
cmd/api/invoicing_test.go                        companies, invitations and row-level security, end to end
db/migrations/20260922000001_row_level_security.sql   the row-level security policies (written by hand, before the module)
db/migrations/20260922000002_invoices.sql        the invoices table, with its own policy (orb gen module --org)
internal/modules/modules.gen.go                  the module list (orb gen modules; don't edit)
internal/modules/invoices/                       the invoices module, as orb gen module --org wrote it
internal/modules/auth/                           sign-in: registration, verification, login, password reset, sessions, MFA, passkeys, API keys
internal/modules/orgs/                           organisations: companies, members, roles and invitations
api/                                             the OpenAPI document, a Postman collection and llms.txt
```

Sign-in (`internal/modules/auth`) and organisations
(`internal/modules/orgs`) are the app's own code, not the library's: `orb
eject` copied them from gorbital so that every step can be read here. Each
is laid out like the app's other modules: the root package with its options
and hooks, then `domain/`, `usecase/` (`register.go`, `verify_email.go`,
`login.go`, `password.go`; `orgs.go`, `members.go`, `invitations.go`),
`repository/` with one SQL statement per file, and `delivery/` with the
routes and jobs, with the tests that came with them. Their migrations are in
`db/migrations` under the versions the library gives them
(`20260915000001_auth.sql`, `20260916000001_orgs.sql`, …), so a database
migrated before the copy applies nothing new, and `gorbital.lock` records
the copy. The packages keep their names, `authhttp` and `orgshttp`, so no
call changed; the API, the tables and the behaviour are the library's.
Library releases no longer change this code, and `orb doctor` warns when the
library's copy does ([Ejecting a
module](../../../docs/guides/ejecting-a-module.md)).

The invoices module is the unchanged output of:

```bash
orb gen module Invoice number:string:unique customer:string 'status:enum(draft,sent,paid,void)' note:text --org
```

## Run it

```bash
orb dev
```

Without the CLI:

```bash
cp .env.example .env
docker compose up -d --wait
set -a; . ./.env; set +a
go run ./cmd/api migrate
go run ./cmd/api
```

Register (`POST /v1/auth/register`), verify the emailed code (with `orb dev`,
in the Dev Portal's mail inbox), sign in, create a company with `POST /v1/orgs` and its
invoices under `/v1/orgs/{orgId}/invoices`.

### With row-level security applied

The compose database's user is a superuser, and PostgreSQL applies no policy
to a superuser: the app warns at startup. To see the policies work, migrate as
that user, then run the app as a role with neither `SUPERUSER` nor
`BYPASSRLS`:

```bash
docker compose exec -T postgres psql -U invoicing -d invoicing <<'SQL'
CREATE ROLE invoicing_app LOGIN PASSWORD 'change-me' NOSUPERUSER NOBYPASSRLS;
GRANT USAGE ON SCHEMA public TO invoicing_app;
GRANT SELECT, INSERT, UPDATE, DELETE, TRUNCATE ON ALL TABLES IN SCHEMA public TO invoicing_app;
GRANT USAGE, SELECT, UPDATE ON ALL SEQUENCES IN SCHEMA public TO invoicing_app;
SQL
DATABASE_URL='postgres://invoicing_app:change-me@127.0.0.1:5432/invoicing?sslmode=disable' go run ./cmd/api
```

Grant again after migrations that add tables.

## Test it

```bash
docker compose up -d --wait
GORBITAL_TEST_DATABASE_URL='postgres://invoicing:invoicing@127.0.0.1:5432/invoicing?sslmode=disable' go test ./...
```

The row-level security test creates the role `invoicing_app_test` on the test
server. After changing a route: `go run ./cmd/api openapi --dir api`. After
adding or removing a module: `go generate ./internal/modules` (or let
`orb dev` do it).
