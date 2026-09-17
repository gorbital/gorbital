# 4. Tests

Shelfie's tests call the API the way its web and mobile apps do: HTTP requests through the whole middleware stack, guards and handlers, on a real PostgreSQL database per test. They use `gorbitaltest` ([Testing with gorbitaltest](../../guides/testing-with-gorbitaltest.md)), so most of them don't sign in: each request says who is calling. One test [signs in for real](#real-sign-in).

## Run them

With `orb dev` running, or `docker compose up -d --wait`:

```bash
export GORBITAL_TEST_DATABASE_URL='postgres://shelfie:shelfie@127.0.0.1:5432/shelfie?sslmode=disable'
export GORBITAL_REQUIRE_DB=1
go test ./...
```

Each test gets a new database next to `shelfie`, migrated with the library's migrations and `db/migrations`, and dropped at the end; your development data is never touched.

## An app per test

`internal/modules/books/books_test.go` builds the app with the options `main.go` passes to `gorbital.Main`:

<!-- include examples/apps/shelfie/internal/modules/books/books_test.go#new-app -->

## Requests as a reader

`gorbitaltest.User` is a signed-in reader holding the permissions you name; `app.As` sends requests as them:

<!-- include examples/apps/shelfie/internal/modules/books/books_test.go#create-and-read -->

`AssertStatus` fails the test with the response body when the status is wrong, and `JSON` decodes the body. The test checks what the domain does to the input: the title trimmed, the ISBN's hyphens removed, the default status.

## Protection

[Chapter 2](02-protecting-routes.md) read the guards on these routes; this is how a module tests them. Deny by default, owners and scopes, for every module:

<!-- include examples/apps/shelfie/internal/modules/books/books_test.go#protection -->

- `app.Client()` has nobody signed in: every books route answers 401 `unauthenticated`.
- Bob gets 404 `book_not_found` for Ada's book, as for a book that doesn't exist, so book IDs can't be probed.
- `gorbitaltest.APIKey` is a request with one of Ada's API keys, scoped to reading: `guard.Permission` refuses the delete with 403 `forbidden`.

`AssertProblem` checks the whole error contract: `application/problem+json`, the status and the `code` clients switch on.

## The rules, as a table

`TestBookRules` sends one invalid request per rule and expects its problem: a blank title is the domain's `title_required`, a missing title is Huma's `validation_failed` from the struct tags, an ISBN already on the shelf is the database's unique index turned into `isbn_taken`.

## Email and jobs

Workers don't run in tests. `app.Mail` returns the email the app queued, and `app.Jobs` the jobs it enqueued, so a test checks both without anything being sent:

<!-- include examples/apps/shelfie/internal/modules/books/books_test.go#mail-and-jobs -->

The books module sends neither yet; the test shows the calls, and a later chapter's welcome email is checked the same way.

## Real sign-in

`gorbitaltest`'s principals stay the default: a test that doesn't pass `gorbital.WithAuth` gets requests as `gorbitaltest.User` and `gorbitaltest.APIKey`. A test that passes `gorbital.WithAuth(authhttp.New())`, as `main.go` does, replaces them with sign-in itself, and signs in the way the web and mobile apps do:

<!-- include examples/apps/shelfie/internal/modules/books/signin_test.go#sign-in -->

- The verification code is read from `app.Mail`: the email is queued, never sent.
- Every account holds the `user` role, which the books module's permissions name, so a new reader can add a book.
- The test's database is migrated with sign-in's migrations too.

## Other tests in the app

| Test | Checks | Needs |
|---|---|---|
| `internal/modules/books/domain/book_test.go` | The rules of `NewBook` and `Apply`, and a fuzz test of ISBN normalisation (`go test -fuzz FuzzNormalizeISBN ./internal/modules/books/domain`) | Nothing |
| `internal/modules/architecture_test.go` | The layers: `domain` imports only the standard library, `delivery` never imports `repository`, a module never imports another module's layers (only its root package, such as an ejected sign-in module's authenticator) | Nothing |
| `internal/modules/books/protection_test.go` | [Chapter 2](02-protecting-routes.md)'s guards: deny by default, a missing permission, the rate limit, and the recent sign-in `DELETE /v1/books` needs | A database |
| `internal/modules/books/client_version_test.go`, `subscription_test.go`, `delivery/require_client_version_test.go` | [Chapter 3](03-your-own-middleware.md)'s middleware and guard, end to end and on their own | The first two, a database |
| `cmd/api/main_test.go` | `api/openapi.json` is what the code describes; after changing a route, run `go run ./cmd/api openapi --dir api` | Go |

Don't add `t.Parallel()` to tests that build an app: two apps built at once in one test binary race on Huma's package-level error constructor.

## Next

[5. Operations](05-operations.md): `/ops`, and a runtime setting and a flag declared by the books module.
