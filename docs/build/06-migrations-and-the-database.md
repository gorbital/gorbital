# 6. Migrations and the database

[Chapter 5](05-the-restaurants-module.md) generated a module and a migration with it. This chapter goes underneath: where that migration runs, why the app's tables and the library's share one numbered history, why forward-only is a rule rather than a preference, and the four small tools the repository layer uses — `postgres.InTx`, the typed constraint helpers, and `pgtest`, which gives every test a database of its own.

gorbital has no ORM, no query builder and no schema DSL. You write SQL. What it gives you is the plumbing around it.

## 1. One history, two owners

**What we're doing.** Understanding where the app's migrations live and what else runs alongside them.

**Why.** Your app's tables and the library's tables are in the same database, and something has to decide the order. Getting that wrong shows up as a foreign key pointing at a table that doesn't exist yet.

**What the framework already gives us.** `gorbital.Migrate` merges the migrations of every library module the app uses with the app's own into **one history**, ordered by version number, and runs it with [goose](https://github.com/pressly/goose) behind a PostgreSQL advisory lock so that two instances starting at once don't both try. River's job tables are applied after. The applied versions are recorded in goose's `goose_db_version` table.

**What we build ourselves.** The `.sql` files under `db/migrations`, and the two-line Go file that embeds them.

**How.** The app's migrations are an `embed.FS`:

<!-- include examples/apps/plateful/db/migrations/migrations.go -->

and `main.go` hands it to the library:

```go
gorbital.WithMigrations(migrations.FS), // db/migrations: the app's own tables
```

Then:

```bash
go run ./cmd/api migrate              # apply everything pending
go run ./cmd/api migrate --status     # report, change nothing
go run ./cmd/api migrate --status --json
```

`orb dev` runs `migrate` for you on every start, and again whenever it sees a changed or new `.sql` file while it is watching — see [chapter 3](03-configuration-and-first-run.md).

**What just happened.** Nine files of Plateful's own joined about a dozen of the library's — settings, jobs, the audit log, rate limits, sign-in's accounts and sessions, organisations, storage — in one ordered list. `migrate --status` reports it as three numbers:

```text
migrations: database at 20260918010090, newest file 20260918010090, 0 pending
```

The library's migration versions are **frozen**. A released migration is never edited, and its version never changes; the table that maps each library module's files to versions in the app's history is append-only. That is what lets your app upgrade the library without the schema shifting under it.

Because the histories are merged, a version collision is possible and is caught rather than ignored. If one of your files has the same version as a library migration and different contents, `migrate` refuses:

```text
gorbital: migration version 20260918000061 differs between …: a released migration never changes; give yours a new version
```

Keep at least one `.sql` file in `db/migrations` while the app is one you can build: `//go:embed *.sql` fails the build with `pattern *.sql: no matching files found` when the directory is empty.

## 2. Versions are UTC timestamps

**What we're doing.** Choosing the number at the front of a migration file name.

**Why.** Two people adding a migration on two branches must not collide, and the order the files apply in must not depend on who merged first.

**What the framework already gives us.** `orb gen migration` picks the version. It is the current time, UTC, formatted `20060102150405` — `YYYYMMDDHHMMSS`. It is then floored to sit above every migration the library has released, and bumped again if a file in `db/migrations` already has a version at or above it.

**What we build ourselves.** The name.

**How.**

```bash
orb gen migration add_restaurant_phone
```

```text
✓ Created migration db/migrations/20260918000071_add_restaurant_phone.sql
```

`AddRestaurantPhone`, `add-restaurant-phone` and `add_restaurant_phone` all produce the same file name. The only flags are `--dry-run`, `--json`, `--allow-dirty`, `--yes`, `--no-input` and `--plain`; like the other generators it refuses a dirty git tree unless you pass `--allow-dirty`, so the new file arrives as a reviewable diff.

**What just happened.** A timestamp puts your migration after everything that exists today and before everything written tomorrow, on any branch, without a counter anyone has to coordinate.

Plateful's own files are numbered `20260918010010` through `20260918010090` — hand-spaced by tens rather than raw timestamps, because they were written in one sitting and the author wanted a readable order. That is allowed: the rule is that the number is unique, sorts correctly, and never changes once released. Timestamps are the default because they satisfy all three without thought.

## 3. Write the SQL *before* you migrate

`orb gen migration` prints this, and it is the sentence to remember from this chapter:

```text
Next:
  1. Write the SQL under -- +goose Up
  2. go run ./cmd/api migrate
  3. go test ./...

Write the SQL before migrating: an empty migration is recorded as applied, and
SQL added to it afterwards never runs. Change the migration freely until it is
released; afterwards, add a new one.
```

The file it wrote is genuinely empty below the header:

```sql
-- Add restaurant phone.
--
-- Change this migration freely until it is released; afterwards, add a new
-- one.

-- +goose Up
```

Run `migrate` now and goose records version `20260918000071` as applied. Write your `CREATE TABLE` into it afterwards and nothing will ever run it — not on your machine, not in CI, not in production. The database will simply be missing a table, and the migration log will insist everything is up to date.

> **Don't do this:** generate a migration, run `orb dev` (which migrates), then fill the SQL in.
> **Do this instead:** generate it, write the SQL, *then* migrate. If you have already applied an empty one on your own machine, `docker compose down -v` and start again — that is cheap in development and impossible in production, which is the whole reason the warning is printed.

## 4. Forward-only, and what that means for `Down`

**What we're doing.** Deciding whether to write a `-- +goose Down` section.

**Why.** "Roll back the deploy" is a comforting idea that does not survive contact with a column you dropped.

**What the framework already gives us.** A rule, stated in [ADR-0005](../adr/0005-database-strategy.md): **released migrations are immutable, and schema changes go forward only.** To undo a released migration, you write a new one that reverses it.

**What we build ourselves.** The reversing migration, when you need one.

The consequences in practice are more nuanced than "there is no Down section", so here they are exactly:

| Where | Down section? |
|---|---|
| A file from `orb gen migration` | **No.** The template stops at `-- +goose Up`. Its comment: *migrations only go forward (ADR-0005)* |
| A migration from `orb gen module` | **Yes** — a `DROP TABLE`. It exists for the window before the migration is released, while you are still iterating on the new module |
| Plateful's own nine migrations | Yes, all `DROP TABLE`, for the same reason |
| The library's released migrations | Never rolled back in production |

`go run ./cmd/api migrate-down` rolls back the most recent migration and **refuses to run when `APP_ENV=production`**:

```text
migrate-down is for development only, and APP_ENV is production
```

One sharp edge worth knowing: a migration **without** a Down section still rolls back — as a *versioned no-op*. goose records it as unapplied while every object it created remains. So `migrate-down` on a file from `orb gen migration` leaves your database claiming a migration is pending while its table already exists, and the next `migrate` will fail. Treat `migrate-down` as a development convenience for the migration you wrote five minutes ago, not as a rollback mechanism.

> **Don't do this:** edit a migration that has run anywhere but your own machine, and re-run `migrate`. goose matches on the version, sees it applied, and your edit is silently ignored — the same failure mode as the empty migration above, arriving months later.
> **Do this instead:** `orb gen migration` a new one. Editing is fine, and expected, right up to the moment the file leaves your branch.

## 5. What a table looks like

Here is the table [chapter 5](05-the-restaurants-module.md) built on:

<!-- include examples/apps/plateful/db/migrations/20260918010020_restaurants.sql#restaurants-table -->

Things to copy from it:

- **`CHECK` constraints mirror the Go validation.** The domain refuses a name over 100 characters and so does the database. Belt and braces, deliberately: the domain gives a good error message to a user, the constraint guarantees the invariant against a bug, a background job or a `psql` session.
- **`org_id text NOT NULL`** is the multi-tenancy marker. The row-level-security migration looks for exactly that, and every statement in the repository filters on it or inserts it — never relying on RLS alone.
- **`UNIQUE (org_id, id)`** looks redundant next to a primary key on `id`. It isn't: it lets *other* organisation tables carry a composite foreign key, so a row can only ever point at a record of its own organisation. `order_lines`' parent does exactly that:

  ```sql
  FOREIGN KEY (org_id, restaurant_id) REFERENCES restaurants (org_id, id) ON DELETE CASCADE
  ```

  A forged `restaurant_id` from another tenant is then not a bug you have to remember to check for; it is a constraint violation.
- **`version bigint NOT NULL DEFAULT 1`** is the optimistic lock, matched in the `UPDATE`'s `WHERE`.
- **`created_by text NOT NULL`, with no foreign key.** It is for display and audit; access comes from membership, and records outlive the account that made them.

The foreign key to `orgs` is added conditionally, and the reason is worth understanding because you will copy the pattern:

```sql
-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass('orgs') IS NOT NULL THEN
        ALTER TABLE restaurants ADD CONSTRAINT restaurants_org_id_fkey
            FOREIGN KEY (org_id) REFERENCES orgs (id) ON DELETE CASCADE;
    END IF;
END
$$;
-- +goose StatementEnd
```

`orgs` belongs to the organisations module. When the app's migrations run with `orgshttp`, it exists and the key is added, so purging an organisation removes its restaurant. When they run without it — another module's test app, built from a smaller set of options — the table is still created, without the key, rather than failing. The `StatementBegin`/`StatementEnd` markers are goose's: a `DO $$ … $$` block contains semicolons, so goose has to be told where the statement ends.

Indexes earn their place the same way. Plateful's late-order sweep runs every few minutes over a table that grows forever, so its index is partial:

<!-- include examples/apps/plateful/db/migrations/20260918010050_orders.sql#orders-late-index -->

And the one table in the whole app with **no `org_id`** carries a comment explaining itself, because a reader will otherwise assume it is a mistake:

<!-- include examples/apps/plateful/db/migrations/20260918010040_couriers.sql#couriers-table -->

## 6. Transactions: `postgres.InTx`

**What we're doing.** Making several writes commit or roll back together.

**Why.** Placing an order writes the order, its lines, the stock the dishes take and the job that tells the restaurant. Four of those and a crash between them is a kitchen that never hears about an order it has already been paid for.

**What the framework already gives us.** One function:

```go
func InTx(ctx context.Context, db Beginner, fn func(tx pgx.Tx) error) error
```

It begins a transaction, runs `fn`, and commits when `fn` returns nil. Otherwise it rolls back and returns `fn`'s error **unchanged**, so `errors.Is` still finds your domain errors. If `fn` panics, it rolls back and lets the panic continue. The rollback uses a context detached from cancellation, so a cancelled request still unwinds cleanly. `InTxWithOptions` takes `pgx.TxOptions` when you need `Serializable`; `postgres.IsRetryable(err)` tells you when to retry one.

**What we build ourselves.** A method on the store that hands the transaction to the layer above as the same interface:

<!-- include examples/apps/plateful/internal/modules/restaurants/repository/store.go#restaurant-store -->

`InTx` on the pool opens a transaction and builds a second `Store` bound to it. That store's `pool` is nil, so a nested `InTx` simply runs `fn` in the transaction already open instead of trying to start a second one. `postgres.InTx` itself does not nest — `pgx.Tx` doesn't satisfy `Beginner` — so this two-line guard is how an app handles it.

> **Don't do this:** keep `tx` anywhere after `fn` returns, or hand it to a goroutine.
> **Do this instead:** build tx-bound repositories *inside* `fn`, as `InTx` above does, and let them go out of scope with the transaction. Also: don't ignore `ctx`. Every query in these layers takes it, so a client that hangs up or a request that hits `APP_REQUEST_TIMEOUT` stops work in the database instead of holding a connection.

The orders module goes one step further and enqueues a background job *inside* the transaction:

<!-- include examples/apps/plateful/internal/modules/orders/repository/tx.go#tx-manager -->

`jobs.Client.InsertTx` writes River's job row through the same `pgx.Tx`. A worker can only pick it up after the transaction commits, and a rollback takes the job with it. That is why placing an order and notifying the restaurant is one operation and not two — and it is the pattern for every "do this, then tell someone" you will write. [Background jobs](../guides/background-jobs.md) is the reference.

## 7. Constraint violations are typed errors

**What we're doing.** Turning a PostgreSQL error into an HTTP status a client can act on.

**Why.** "Duplicate key value violates unique constraint" is not an API. `409 restaurant_name_taken` is.

**What the framework already gives us.** Six helpers in `gorbital.dev/modules/postgres`, each unwrapping the driver's `*pgconn.PgError` for you:

| Helper | Returns | SQLSTATE |
|---|---|---|
| `IsNoRows(err) bool` | A query returned no rows | — |
| `UniqueViolation(err) (constraint string, ok bool)` | The **constraint name** | 23505 |
| `ForeignKeyViolation(err) (constraint string, ok bool)` | The constraint name | 23503 |
| `CheckViolation(err) (constraint string, ok bool)` | The constraint name | 23514 |
| `NotNullViolation(err) (column string, ok bool)` | The **column** name | 23502 |
| `IsRetryable(err) bool` | A serialization failure or deadlock | 40001, 40P01 |

The constraint name is the point. One table can have several unique indexes, and each becomes a different API error.

**What we build ourselves.** One function per repository, mapping only the constraints the use cases actually handle:

<!-- include examples/apps/plateful/internal/modules/restaurants/repository/store.go#restaurant-constraint-error -->

`restaurants_name` and `restaurants_org_id_key` are two unique constraints on one table, and they mean two different things to a caller: a name somebody else already uses, and a race between two first saves of the same organisation's profile. Both are `409`, with different codes.

`IsNoRows` does the other half — including the optimistic lock, where "no row matched" is not "not found" but "the version moved":

<!-- include examples/apps/plateful/internal/modules/restaurants/repository/update_restaurant.go#update-restaurant-sql -->

> **Don't do this:** `SELECT` to check whether a name is free, then `INSERT`. Between the two, someone else inserts it.
> **Do this instead:** insert, and map the unique violation. The database is the only thing that can answer that question without a race.

Give constraints names you can read, because those names are what your Go code matches on. `restaurants_name` is a constraint name in a switch statement; rename the index and the mapping falls through silently — every test still passes and the API starts returning `500`.

## 8. A database per test

**What we're doing.** Running the module's tests against real PostgreSQL.

**Why.** The repository layer is hand-written SQL. A fake proves nothing about it, and neither does a mock of `pgx`.

**What the framework already gives us.** `gorbital.dev/modules/postgres/pgtest`. It creates a **database per test**, migrated, and drops it when the test ends. It does that fast by building a *template* database once per distinct set of migrations — named from a hash of the migration files — and cloning it with `CREATE DATABASE … TEMPLATE …`. Nothing is truncated between tests and nothing is shared, so tests can run in parallel without a shared-fixture problem.

Its API is small:

| Function | Purpose |
|---|---|
| `pgtest.New(t, opts...) *pgxpool.Pool` | A pool on a fresh database |
| `pgtest.NewDatabase(t, opts...) string` | Its URL, for tests that build a whole app from configuration |
| `pgtest.URL(t) string` | The server URL |
| `pgtest.WithMigrations(fsys fs.FS)` | Apply these migrations to it |
| `pgtest.WithMaxConns(n int32)` | Pool size; default 4 |

It reads two environment variables:

| Variable | Effect |
|---|---|
| `GORBITAL_TEST_DATABASE_URL` | The PostgreSQL server. **Unset: database tests skip** |
| `GORBITAL_REQUIRE_DB=1` | A missing URL fails instead of skipping |

**What we build ourselves.** Usually nothing — Plateful never imports `pgtest`. Its tests go through `gorbitaltest`, which calls `pgtest.NewDatabase` for you:

```go
func newApp(t *testing.T) *gorbitaltest.App {
	t.Helper()
	auth := authhttp.New()
	return gorbitaltest.New(t,
		gorbital.WithAuth(auth),
		gorbital.WithModules(orgshttp.Module(auth), restaurants.Module()),
		gorbital.WithMigrations(migrations.FS),
	)
}
```

That is the whole setup for the tests in [chapter 5](05-the-restaurants-module.md): a real app, real middleware, real guards, real migrations, its own database. Reach for `pgtest` directly when you want to test a repository without an HTTP stack around it.

**How to run them.**

```bash
docker compose up -d --wait
GORBITAL_TEST_DATABASE_URL='postgres://plateful:plateful@127.0.0.1:5432/plateful?sslmode=disable' \
  go test -race ./...
```

> **Don't do this:** claim the tests pass after a run where the variable wasn't set. Every database test **skips**, silently, and `go test` prints `ok`.
> **Do this instead:** set `GORBITAL_REQUIRE_DB=1` in CI and whenever you are about to say "tests pass". A missing URL then fails the run instead of hiding it.

Old templates stay until `docker compose down -v`. If a migration change ever seems not to take effect in tests, that is the thing to reset.

## What just happened

Your tables and the library's share one numbered, forward-only history that neither side can edit behind the other's back. New migrations get a timestamp that can't collide. Transactions are one function with no lifecycle to manage. PostgreSQL's constraint violations arrive as named things you can map to API errors. And every test gets a real, migrated, private database in a few milliseconds.

None of that is an abstraction over SQL. You still write every statement, name every index, and decide every constraint — which is the trade gorbital makes throughout: it owns the plumbing and leaves the schema to you.

Further reading: [Database](../guides/database.md) is the full reference for `postgres.Open`, the repository conventions, the error helpers and a Full app's whole schema; [Methods: modules/postgres](../methods/modules-postgres.md) is the generated API reference; [Row-level security](../guides/row-level-security.md) covers the policy Plateful ships but doesn't apply.
