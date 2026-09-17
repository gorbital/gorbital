# Testing with gorbitaltest

`gorbital.dev/gorbital/gorbitaltest` tests an app the way clients use it: HTTP requests through the whole middleware stack, guards and handlers, on a real PostgreSQL database of its own per test. Tests don't sign in: they say who is calling. [Methods](../methods/gorbital-gorbitaltest.md) lists every function; [Testing](testing.md) covers the rest of gorbital's testing (`pgtest`, fakes for sign-in providers, what CI runs).

## Set up

Tests need a PostgreSQL server. With `orb dev` running, or `docker compose up -d --wait`:

```bash
export GORBITAL_TEST_DATABASE_URL='postgres://shelfie:shelfie@127.0.0.1:5432/shelfie?sslmode=disable'
export GORBITAL_REQUIRE_DB=1   # fail instead of skipping when the variable is missing
go test ./...
```

The URL names a server, not a database: every test gets a new `pgtest_…` database next to yours, migrated and then dropped. Without `GORBITAL_TEST_DATABASE_URL`, the tests are skipped with instructions.

## An app per test

<!-- include examples/apps/shelfie/internal/modules/books/books_test.go#new-app -->

`gorbitaltest.New` takes the options your `main.go` passes to `gorbital.Main`. For each test it:

1. creates a database;
2. runs `gorbital.Migrate`: the library's migrations, your modules' and `db/migrations`, then River's;
3. builds the app with `gorbital.New`, in development, with warnings written to the test's output and temporary directories for files and logs;
4. closes the app and drops the database when the test ends.

It takes about 0.2 s on a laptop, so every test can have its own app. Don't mark tests that build an app with `t.Parallel()`: Huma keeps its error constructor in a package-level variable, which each app sets to its own error mappings (`openapi.InstallErrors`, [ADR-0027](../adr/0027-api-contract-and-docs.md)), so two apps built at once in one test binary race. Tests in different packages run in separate processes and are unaffected.

## Requests

<!-- include examples/apps/shelfie/internal/modules/books/books_test.go#create-and-read -->

| Call | Sends |
|---|---|
| `app.Client()` | Requests with nobody signed in |
| `app.As(gorbitaltest.User("usr_ada", perms...))` | Requests from a signed-in user holding `perms`, with a session that signed in and verified a second factor just now |
| `app.As(gorbitaltest.APIKey("usr_ada", scopes...))` | Requests authenticated with one of the user's API keys, limited to `scopes`; guards that need a session, such as `guard.RecentReauth`, refuse it |
| `.Get(path)`, `.Delete(path)` | A request without a body |
| `.Post(path, body)`, `.Put(path, body)`, `.Patch(path, body)` | `body` encoded as JSON, with `Content-Type: application/json` |
| `.WithHeader("Idempotency-Key", "k1")` | A client that adds the header to every request |
| `.Do(req)` | Your own `*http.Request`, with the client's principal and headers |

A principal is put in the request's context exactly as an authenticator puts it, before the stack runs, so guards, `actor.From` in use cases, audit events and idempotency keys all see it. The permissions are the ones you pass: the test says what the caller holds, and roles don't come into it.

Tests don't need an authenticator: gorbitaltest's own passes requests on. When a test passes `gorbital.WithAuth`, that authenticator runs instead, which is how sign-in's own tests run.

`gorbitaltest.NewWithEnv(t, env, opts...)` sets environment variables on top of development's defaults, as `.env` would, such as `AUTH_ENCRYPTION_KEYS` for tests that turn on an authenticator app; the process environment is never read. `app.Config()` is the configuration the app was built with, for running a command against the test's database, such as `authhttp`'s `grant-role`:

```go
auth := authhttp.New()
app := gorbitaltest.NewWithEnv(t, map[string]string{"AUTH_ENCRYPTION_KEYS": keys}, gorbital.WithAuth(auth), gorbital.WithModules(opshttp.Module()))
for _, c := range auth.Commands() {
	if c.Name == "grant-role" {
		err := c.Run(ctx, app.Config(), []string{"ops@example.com", "platform_admin"}, io.Discard)
	}
}
```

`gorbital/internal/integration` signs an operator in this way, with an authenticator app and a second factor, and checks `/ops` end to end.

## Assertions

| Method | Fails the test unless |
|---|---|
| `res.AssertStatus(t, http.StatusCreated)` | The status is 201; the failure shows the body |
| `res.AssertProblem(t, http.StatusNotFound, "book_not_found")` | The response is `application/problem+json` with that status and `code` |
| `res.JSON(t, &v)` | The body decodes into `v` |

`res.Status`, `res.Header` and `res.Body` are there for anything else.

<!-- include examples/apps/shelfie/internal/modules/books/books_test.go#protection -->

Test deny by default for every module: a route that should need sign-in answers 401 to `app.Client()`, and one that needs a permission answers 403 to a caller without it.

## Email and jobs

Background workers don't run in tests, so nothing is delivered. What the app queued is read back from the job table instead:

| Method | Returns |
|---|---|
| `app.Mail(t)` | Every email modules sent through `Deps.Mailer`, oldest first, with the sender filled in from the `mail.*` settings |
| `app.Jobs(t, kind)` | Every job of `kind` enqueued through `Deps.Jobs`, oldest first, with its JSON arguments; `""` for all |

<!-- include examples/apps/shelfie/internal/modules/books/books_test.go#mail-and-jobs -->

To test what a job does, call its worker's `Work` directly in a test of its package.

## Reaching the database

`app.App()` is the `*gorbital.App`: `app.App().Deps().DB` is its pool, for preparing rows or checking what a request wrote, such as its audit events:

```go
var events int
err := app.App().Deps().DB.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action = 'books.book.created'`).Scan(&events)
```

## What gorbitaltest isn't for

- **Domain rules**: test `domain/` with plain table tests; they need no database.
- **SQL edge cases** of one repository method: a `pgtest.New(t, pgtest.WithMigrations(...))` pool and the store are enough.
- **Sign-in itself**: requests carry principals, so the session and API key checks of the authenticator aren't exercised. Those are sign-in's tests.

## Related

- [Shelfie, chapter 4](../examples/shelfie/04-tests.md): the books module's tests, step by step.
- [Your main.go](main-go.md): the options `New` takes.
- [Testing](testing.md): `pgtest`, CI, and v0.1 apps' tests.
