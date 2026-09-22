# Add your first module

A **module** is a part of your app with its own data and endpoints, such as invoices, customers or tasks. `orb gen module` writes one for you: the database table, the rules, the endpoints with their protection, and the tests. The result is ordinary Go code in your repository, which you then change like any other code.

This page adds invoices to the app from the [Quickstart](quickstart.md) and explains every file. The output below comes from a real run.

> [!NOTE]
> This page is for apps on `gorbital.Main`, which `orb new` creates from v0.2 on. An app created by orb v0.1 keeps its layout (`internal/app`): there, `orb gen resource` writes the module and wires it into `internal/app` ([v0.1 docs](https://docs.gorbital.dev/v0.1/guides/first-resource)). In an app on `gorbital.Main`, `orb gen resource` runs `orb gen module`.

## Before you start

- `orb dev` is running in Terminal 1, or at least `docker compose up -d --wait`.
- Your changes are committed. `orb gen` refuses to run otherwise, so its changes are a clean diff: `git status --short` should print nothing.

## 1. Describe the records

A module has a **name** (singular) and **fields**. Each field is `name:type`, with `:unique` for values that must differ:

| Field type | Stores | Rules |
|---|---|---|
| `string` | Short text, such as a name or number | 1 to 100 characters, required, sortable |
| `string:unique` | The same, unique per owner, ignoring upper and lower case | Duplicates get 409 `<record>_<field>_taken` |
| `string?` | Short text that may be empty, such as a nickname | 0 to 100 characters, optional, sortable, never unique |
| `text` | Long text, such as notes | Up to 2000 characters, optional |
| `enum(a,b,c)` | One value from a fixed list | The first value is the default; lists can filter by it |

A module needs at least one required `string` field. The first one is its title.

## 2. Generate it

In **Terminal 2**, in your app's folder:

```bash
orb gen module Invoice number:string:unique 'status:enum(draft,sent,paid)' notes:text
```

Quote the enum field: shells treat parentheses specially. Leave the fields out to be asked for them. Add `--dry-run` first to see the files without writing them, or `--diff` to see them as a diff.

```text
✓ Created module invoices

  Module:      invoices (table invoices, IDs like inv_…)
  API:         /v1/invoices, for the signed-in user's invoices
  Permissions: invoices.invoice.read, invoices.invoice.write (the user role)
  Fields:
    number               string, 1 to 100 characters, unique
    status               one of draft, sent, paid (default draft)
    notes                text, up to 2000 characters
  Files:
    create internal/modules/invoices/module.go
    create internal/modules/invoices/invoices_test.go
    create internal/modules/invoices/domain/invoice.go
    …
    create internal/modules/invoices/delivery/routes.go
    …
    create db/migrations/20260917165619_invoices.sql
    modify internal/modules/modules.gen.go

Next:
  1. go run ./cmd/api migrate (orb dev runs it)
  2. go run ./cmd/api openapi --dir api
  3. go test ./internal/modules -run TestPublicSurface -update (records the new error codes, audit actions and permissions in api/surface.json)
  4. go test ./internal/modules/invoices/...
  5. go run ./cmd/api, sign in, then POST /v1/invoices
```

It never overwrites a file, and it doesn't touch `cmd/api/main.go`: `main.go` adds every module listed in `internal/modules/modules.gen.go` with `gorbital.WithModules(modules.All()...)`.

## 3. What it created

Your new module is split into four **layers**. Each has one job, and a request passes through them in order:

```text
HTTP request
   │
   ▼
delivery/     the route table and its guards, reads the request, writes the response  (knows HTTP, not SQL)
   │
   ▼
usecase/      applies the steps for the signed-in user                                (knows neither HTTP nor SQL)
   │
   ▼
domain/       the rules: what a valid invoice is                                      (plain Go, no dependencies)
   │
   ▼
repository/   reads and writes the database                                           (knows SQL, not HTTP)
   │
   ▼
PostgreSQL
```

Keeping them apart means you can change the rules without touching SQL, or the SQL without touching the endpoints. Each operation (create, get, list, update, delete) has its own file in each layer, so a change to one operation is a change to its files. `internal/modules/architecture_test.go` fails when a layer imports one it shouldn't.

| File | What it's for |
|---|---|
| `module.go` | `func Module() gorbital.Module`: the module's name, its error codes (each domain error mapped to an HTTP status and code), its permissions and the roles that hold them, and its routes |
| `domain/invoice.go` | The `Invoice` type and its rules: lengths, allowed statuses, what an update may change. Put new business rules here |
| `domain/errors.go` | Errors such as "not found" and "number taken", with no HTTP in them |
| `domain/invoice_test.go` | Tests for the rules; no database needed |
| `usecase/service.go`, `usecase/ports.go` | The `Service`, and what it needs from the outside as small interfaces: a store and an audit recorder |
| `usecase/create_invoice.go`, `get_invoice.go`, `list_invoices.go`, `update_invoice.go`, `delete_invoice.go` | One operation each, for the signed-in owner, recording an audit event for changes |
| `repository/store.go` | The `Store`, which works on the database pool or inside a transaction |
| `repository/insert_invoice.go`, `select_invoice.go`, `select_invoices.go`, `update_invoice.go`, `delete_invoice.go` | One SQL statement per file, next to the Go that runs it |
| `delivery/routes.go` | The route table: every route with its guard, such as `guard.Permission(usecase.PermWrite)` |
| `delivery/responses.go`, `delivery/create_invoice.go`, … | The response shape, and each operation's input, output and handler |
| `invoices_test.go` | HTTP tests through the app's real middleware stack on a temporary database: create, list with pages and filters, update with versions, delete, another user getting 404, a read-only API key refused, and the audit events |
| `db/migrations/20260917165619_invoices.sql` | Creates the table |
| `internal/modules/modules.gen.go` | Rewritten to list the module. Never edit it: `orb gen modules` (and `orb dev`) write it |

Here is the route table:

```go
func Register(r *gorbital.Router, svc *usecase.Service) {
	h := handlers{svc: svc}
	invoices := r.Group("/v1/invoices", gorbital.Tags("Invoices"))

	gorbital.Post(invoices, "", h.createInvoice, gorbital.OperationID("invoices-create"),
		gorbital.Summary("Create an invoice"), gorbital.Status(http.StatusCreated),
		gorbital.Errors(http.StatusConflict, http.StatusUnprocessableEntity),
		guard.Permission(usecase.PermWrite))
	gorbital.Get(invoices, "", h.listInvoices, …, guard.Permission(usecase.PermRead))
	…
}
```

Every route requires a signed-in caller unless it says `guard.Public()`, so a route can't be left open by mistake. The permissions `invoices.invoice.read` and `.write` are held by the `user` role every account has: signed-in sessions always have them; an [API key](../guides/api-keys.md) only when its scopes include them. `orb routes` lists every route of the app with its guards.

## 4. The table

The migration is plain SQL. The file name starts with the time it was created, so it runs after every existing migration:

```sql
-- +goose Up
CREATE TABLE invoices (
    id         text        PRIMARY KEY,
    -- The signed-in user who owns the invoice; only they can read or change it.
    owner_id   text        NOT NULL,
    number     text        NOT NULL CHECK (char_length(number) BETWEEN 1 AND 100),
    status     text        NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'sent', 'paid')),
    notes      text        NOT NULL DEFAULT '' CHECK (char_length(notes) <= 2000),
    -- Increases with every update; an update must send the version it read.
    version    bigint      NOT NULL DEFAULT 1,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

-- Number is unique per owner, ignoring case.
CREATE UNIQUE INDEX invoices_owner_number ON invoices (owner_id, lower(number));

-- One index per sort of GET /v1/invoices.
CREATE INDEX invoices_owner_created ON invoices (owner_id, created_at, id);
CREATE INDEX invoices_owner_updated ON invoices (owner_id, updated_at, id);
CREATE INDEX invoices_owner_number_sort ON invoices (owner_id, (lower(number) COLLATE "C"), id);

-- +goose Down
DROP TABLE invoices;
```

What the columns are for:

- **`id`** looks like `inv_…`: a prefix, so you can tell what an ID refers to, and 128 random bits.
- **`owner_id`** is the user who created the invoice. Every query filters on it, so one user can never read another's invoices.
- **`version`** stops lost updates. Updates send the version they read; if someone changed the invoice in between, the update fails with 409 instead of silently overwriting their change.
- **The `CHECK` constraints** repeat the domain rules in the database, so bad data can't get in even from a script.

Your app's migrations run in one history with gorbital's own (accounts, sessions, settings, jobs), ordered by version.

## 5. Apply, record, test, export

If `orb dev` is running, it notices the new migration, applies it and restarts: `/docs` already shows **Invoices**. Otherwise, with the environment loaded:

```bash
go run ./cmd/api migrate
```

Record the module's public names (its error codes, audit actions and permissions) in `api/surface.json`. They are part of your API: `TestPublicSurface` fails when one disappears or a new one isn't recorded, so a rename shows up in review:

```bash
go test ./internal/modules -run TestPublicSurface -update
```

Run the tests. They create their own temporary databases, so your development data isn't touched:

```bash
export GORBITAL_TEST_DATABASE_URL='postgres://acme-api:acme-api@127.0.0.1:5432/acme-api?sslmode=disable'
export GORBITAL_REQUIRE_DB=1
go test ./...
```

`GORBITAL_REQUIRE_DB=1` makes database tests fail rather than skip when the database is missing, so a green run means they really ran.

Update the API description that's committed with your code, so API changes show up in reviews (`TestOpenAPIIsCurrent` in `cmd/api` checks it):

```bash
go run ./cmd/api openapi --dir api
```

## 6. Try it

With a token from the [Quickstart](quickstart.md#5-create-an-account):

```bash
curl -X POST http://127.0.0.1:8080/v1/invoices -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"number": "2026-001"}'

curl "http://127.0.0.1:8080/v1/invoices?status=draft&sort=-created_at" -H "Authorization: Bearer $TOKEN"
```

The first creates an invoice with status `draft`. The second lists your drafts, newest first. Send the same number again and you get `409` with code `invoice_number_taken`.

## 7. Commit

```bash
git add -A
git commit -m "Add invoices"
```

## Change it

The code is yours. Common changes:

| To | Edit |
|---|---|
| Add a rule, such as "a paid invoice can't be changed" | `domain/invoice.go`, and return a new error from `domain/errors.go`; map it in the module's `Errors` in `module.go` |
| Add an endpoint | The use case in its own `usecase/` file, its input, output and handler in its own `delivery/` file, and the route with its guard in `delivery/routes.go` |
| Protect a route differently | Its guards in `delivery/routes.go`: `guard.RateLimit(30, time.Minute)`, `guard.RecentReauth()`, or your own with `orb gen middleware <Name> --module invoices --guard` ([Guards](../guides/guards-and-middleware.md)) |
| Add a column | A new migration: `orb gen migration add_invoice_due_date`, then the domain type, the SQL files that read or write it, and the delivery files |
| Change a query | The SQL constant in the repository file for that operation |
| Change a response | The types in `delivery/`, then export `api/` again |

Edit a migration only until it has run anywhere but your computer. After that, add a new one: a database never runs the same migration twice. Locally, `docker compose down -v` resets the database if you need to rerun an edited one.

## In a multi-tenant app

In an app created with a named scope (`--scope organisation`, or a word of your own), add `--scope tenant` to make invoices belong to the **tenant** instead of a user: endpoints under `/v1/orgs/{orgId}/invoices` guarded by `guard.OrgMember`, an `org_id` column instead of `owner_id`, `invoices.invoice.read` and `.write` held by the tenant's roles, and tests proving a member of another tenant gets 404. `--org` is the older name of `--scope tenant` and still works, and `orb gen resource` does the same without the flag in such an app. The other values are `--scope user` (the default in an app without a tenancy) and `--scope public`; what each one writes is in [Resource access](../guides/resource-access.md). See also [Tenancy](../guides/tenancy.md) and [Organisations](organisations.md).

All flags and rules: [CLI reference](../guides/cli.md#orb-gen-module), [Generating code](../guides/generating-code.md).
