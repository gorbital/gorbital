# Add your first resource

A **resource** is a kind of data in your API, such as invoices, customers or tasks, with endpoints to create, read, update and delete it. `orb gen resource` writes one for you: the database table, the rules, the endpoints and the tests. The result is ordinary Go code in your repository, which you then change like any other code.

This page adds invoices to the app from the [Quickstart](quickstart.md) and explains every file. The output below comes from a real run.

## Before you start

- `orb dev` is running in Terminal 1, or at least `docker compose up -d --wait`.
- Your changes are committed. `orb gen` refuses to run otherwise, so its changes are a clean diff: `git status --short` should print nothing.

## 1. Describe the resource

A resource has a **name** (singular) and **fields**. Each field is `name:type`, with `:unique` for values that must differ:

| Field type | Stores | Rules |
|---|---|---|
| `string` | Short text, such as a name or number | 1 to 100 characters, required, sortable |
| `string:unique` | The same, unique per owner, ignoring upper and lower case | Duplicates get 409 `<resource>_<field>_taken` |
| `text` | Long text, such as notes | Up to 2000 characters, optional |
| `enum(a,b,c)` | One value from a fixed list | The first value is the default; lists can filter by it |

A resource needs at least one `string` field. The first one is its title.

## 2. Generate it

In **Terminal 2**, in your app's folder:

```bash
orb gen resource Invoice number:string:unique 'status:enum(draft,sent,paid)' notes:text
```

Quote the enum field: shells treat parentheses specially. Leave the fields out to be asked for them one at a time. Add `--dry-run` first to see the files without writing them.

```text
✓ Created resource Invoice

  Resource:  Invoice (table invoices, IDs like inv_…)
  API:       /v1/invoices, for the signed-in user's invoices
  Fields:
    number               string, 1 to 100 characters, unique
    status               one of draft, sent, paid (default draft)
    notes                text, up to 2000 characters
  Files:
    internal/modules/invoices/module.go
    internal/modules/invoices/domain/invoice.go
    …
    db/migrations/20260915140945_invoices.sql
    internal/app/modules.go

Next:
  1. go run ./cmd/migrate
  2. go test ./...
  3. go run ./cmd/api openapi > api/openapi.json
  4. go run ./cmd/api, sign in, then POST /v1/invoices
```

## 3. What it created

Your new module is split into four **layers**. Each has one job, and a request passes through them in order:

```text
HTTP request
   │
   ▼
delivery/     reads the request and writes the response      (knows HTTP, not SQL)
   │
   ▼
usecase/      checks who's asking and applies the steps      (knows neither HTTP nor SQL)
   │
   ▼
domain/       the rules: what a valid invoice is            (plain Go, no dependencies)
   │
   ▼
repository/   reads and writes the database                  (knows SQL, not HTTP)
   │
   ▼
PostgreSQL
```

Keeping them apart means you can change the rules without touching SQL, or the SQL without touching the endpoints, and test each alone.

| File | What it's for |
|---|---|
| `internal/modules/invoices/domain/invoice.go` | The `Invoice` type and its rules: lengths, allowed statuses, what an update may change. Put new business rules here |
| `internal/modules/invoices/domain/errors.go` | Errors such as "not found" and "number taken", with no HTTP in them |
| `internal/modules/invoices/domain/invoice_test.go` | Tests for the rules; no database needed |
| `internal/modules/invoices/usecase/ports.go` | What the use cases need from the outside, as small interfaces: a store, a transaction runner, an audit recorder |
| `internal/modules/invoices/usecase/service.go` | The `Service` that holds those dependencies |
| `internal/modules/invoices/usecase/invoices.go` | Create, get, list, update and delete, each for the signed-in owner, each recording an audit event |
| `internal/modules/invoices/usecase/invoices_test.go` | Tests for the use cases against a real PostgreSQL |
| `internal/modules/invoices/repository/store.go` | The `Store`, which works on the database pool or inside a transaction |
| `internal/modules/invoices/repository/insert_invoice.go`, `select_invoice.go`, `select_invoices.go`, `update_invoice.go`, `delete_invoice.go` | One SQL statement per file, next to the Go that runs it |
| `internal/modules/invoices/repository/scan.go` | Turns database rows into `Invoice` values |
| `internal/modules/invoices/repository/store_test.go` | Tests for every query against a real PostgreSQL |
| `internal/modules/invoices/delivery/invoices.go` | The endpoints: request and response shapes, documentation for `/docs`, status codes |
| `internal/modules/invoices/module.go` | Connects the four layers |
| `internal/app/module_invoices.go` | Builds the module when the app starts, and maps each domain error to an HTTP status and code |
| `internal/app/invoices_test.go` | A full HTTP test: sign up, create, list, update, delete, and proof that another user gets 404 for your invoices |
| `internal/app/modules.go` | Changed by one line, `registerInvoices(…)`, so the app includes the module |
| `db/migrations/20260915140945_invoices.sql` | Creates the table |

## 4. The table

The migration is plain SQL. The file name starts with the time it was created, so it runs after every existing migration:

```sql
-- +goose Up
CREATE TABLE invoices (
    id         text        PRIMARY KEY,
    -- Purging an account (after its retention) deletes its invoices.
    owner_id   text        NOT NULL REFERENCES auth_users (id) ON DELETE CASCADE,
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
```

What the columns are for:

- **`id`** looks like `inv_…`: a prefix, so you can tell what an ID refers to, and 128 random bits.
- **`owner_id`** is the user who created the invoice. Every query filters on it, so one user can never read another's invoices.
- **`version`** stops lost updates. Updates send the version they read; if someone changed the invoice in between, the update fails with 409 instead of silently overwriting their change.
- **The `CHECK` constraints** repeat the domain rules in the database, so bad data can't get in even from a script.

## 5. Apply, test, export

If `orb dev` is running, it notices the new migration, applies it and restarts: `/docs` already shows **Invoices**. Otherwise, with the environment loaded:

```bash
go run ./cmd/migrate
```

Run the tests. They create their own temporary databases, so your development data isn't touched:

```bash
export GORBITAL_TEST_DATABASE_URL='postgres://acme-api:acme-api@127.0.0.1:5432/acme-api?sslmode=disable'
export GORBITAL_REQUIRE_DB=1
go test ./...
```

`GORBITAL_REQUIRE_DB=1` makes database tests fail rather than skip when the database is missing, so a green run means they really ran.

Update the API description that's committed with your code, so API changes show up in reviews:

```bash
go run ./cmd/api openapi > api/openapi.json
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
| Add a rule, such as "a paid invoice can't be changed" | `domain/invoice.go`, and return a new error from `domain/errors.go`; map it in `internal/app/module_invoices.go` |
| Add a column | A new migration: `orb gen migration add_invoice_due_date`, then the domain type, the SQL files that read or write it, and `delivery/invoices.go` |
| Change a query | The SQL constant in the repository file for that operation |
| Change a response | The types in `delivery/invoices.go`, then export `api/openapi.json` again |

Edit a migration only until it has run anywhere but your computer. After that, add a new one: a database never runs the same migration twice. Locally, `docker compose down -v` resets the database if you need to rerun an edited one.

## In a multi-tenant app

In an app created with `--tenancy multi`, the same command makes invoices belong to an **organisation** instead of a user: endpoints under `/v1/orgs/{orgId}/invoices`, an `org_id` column instead of `owner_id`, a membership check in every use case, `invoices.invoice.read` and `.write` permissions for organisation roles, and tests proving a member of another organisation gets 404. See [Organisations](organisations.md).

All flags and rules: [CLI reference](../guides/cli.md#orb-gen-resource).
