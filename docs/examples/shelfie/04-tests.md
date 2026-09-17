# 4. Tests

Shelfie's tests call the API the way its web and mobile apps do: HTTP requests through the whole middleware stack, guards and handlers, on a real PostgreSQL database per test. They use `gorbitaltest` ([Testing with gorbitaltest](../../guides/testing-with-gorbitaltest.md)), so they don't need sign-in to exist: each request says who is calling.

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

Test deny by default, owners and scopes for every module:

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

## Other tests in the app

| Test | Checks | Needs |
|---|---|---|
| `internal/modules/books/domain/book_test.go` | The rules of `NewBook` and `Apply`, and a fuzz test of ISBN normalisation (`go test -fuzz FuzzNormalizeISBN ./internal/modules/books/domain`) | Nothing |
| `internal/modules/architecture_test.go` | The layers: `domain` imports only the standard library, `delivery` never imports `repository`, modules never import each other | Nothing |
| `cmd/api/main_test.go` | `api/openapi.json` is what the code describes; after changing a route, run `go run ./cmd/api openapi --dir api` | Go |

Don't add `t.Parallel()` to tests that build an app: two apps built at once in one test binary race on Huma's package-level error constructor.

## Next

Chapter 5 adds operations: `/ops`, and runtime settings and flags declared by the books module (Phase 4).
