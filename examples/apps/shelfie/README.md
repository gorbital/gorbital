# Shelfie

A reading-tracker API for a web and a mobile app: the example application
of gorbital's Examples tab, built chapter by chapter. Every code block on
those pages is included from this directory.

```text
cmd/api/main.go                 gorbital.Main with the app's modules, organisations and migrations
cmd/api/signin.go               sign-in's options and hooks, and the SMS sender
db/migrations/                  the app's migrations, sign-in's and organisations' included
internal/modules/modules.gen.go the module list (orb gen modules; don't edit)
internal/modules/books/         a module: module.go, domain/, usecase/, repository/, delivery/
internal/modules/profiles/      readers' profiles, filled at registration (chapter 6)
internal/modules/phonelogin/    phone-code sign-in through authhttp's SignIn (chapter 7)
internal/modules/shelves/       readers' shelves, as orb gen module wrote them (chapter 9)
internal/modules/clubbooks/     book clubs' reading lists, as orb gen module --org wrote them (chapter 8)
internal/modules/auth/          sign-in: registration, verification, login, password reset, sessions, MFA, passkeys, API keys
internal/modules/orgs/          organisations: book clubs, members, roles and invitations
api/                            the OpenAPI document, a Postman collection and llms.txt
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

Chapter 6's registration fields and hooks and chapter 7's phone sign-in
are `signin.go`, `profiles` and `phonelogin`, unchanged: they use the
copy's options and `Authenticator.SignIn` as they used the library's.

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

Readers register with `POST /v1/auth/register`, which also takes a
`display_name` (chapter 6). In development, phone sign-in codes are written
to the log instead of texted (chapter 7). A book club is an organisation:
`POST /v1/orgs`, then invitations, and its reading list under
`/v1/orgs/{orgId}/club-books` (chapter 8).

## Test it

```bash
docker compose up -d --wait
GORBITAL_TEST_DATABASE_URL='postgres://shelfie:shelfie@127.0.0.1:5432/shelfie?sslmode=disable' go test ./...
```

After changing a route: `go run ./cmd/api openapi --dir api`. After adding
or removing a module: `go generate ./internal/modules` (or let `orb dev` do
it).
