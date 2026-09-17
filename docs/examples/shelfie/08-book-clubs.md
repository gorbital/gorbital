# 8. Book clubs

Readers meet in book clubs: the Thursday club at the library, a group of friends reading one novel a month. A club has members, an owner who invites them, and a shared reading list that every member keeps and nobody else can see. In gorbital a club is an **organisation**: this chapter adds the organisations module, generates the reading list as a module whose records belong to an organisation, and shows the layers that keep one club's list out of another's ([Organisations](../../start/organisations.md#in-an-app-on-gorbitalmain)).

| Club | In gorbital |
|---|---|
| A book club | An organisation, `POST /v1/orgs` |
| Its owner, admins and members | Members with the roles `owner`, `admin` and `member` |
| Joining | An invitation emailed by an owner or admin, accepted with `POST /v1/invitations/accept` |
| The reading list | Club books, `/v1/orgs/{orgId}/club-books`, generated with `orb gen module --org` |
| A reader alone | Their personal workspace, the organisation every account gets |

## 1. The organisations module

Organisations are a built-in module, `gorbital.dev/gorbital/orgshttp`. Its members are sign-in's accounts, so `orgshttp.Module` takes the authenticator `main.go` already passes to `gorbital.WithAuth`, and goes on its own line like phone sign-in's module ([chapter 7](07-phone-code-sign-in.md)):

<!-- include examples/apps/shelfie/cmd/api/main.go#main -->

With that line, Shelfie serves v0.1's organisation API unchanged, under `/v1/orgs` and `/v1/invitations`, and its two migrations join the app's history.

> [!WARNING]
> The organisations module's migrations keep the versions v0.1 apps hold them under, `20260916000001` and `20260918000002`, earlier than chapters 1 to 7's. A local database already migrated past them refuses to migrate them out of order (goose reports missing migrations). Recreate the development database (`docker compose down -v`, then `orb dev`), as the tests do with a fresh database each time. A database you can't recreate keeps its data: apply the two files and record them in `goose_db_version` by hand — [Adding organisations to a database that already exists](../../start/organisations.md#adding-organisations-to-a-database-that-already-exists).

What it serves:

| Route | Does |
|---|---|
| `GET /v1/orgs`, `POST /v1/orgs` | Lists the reader's clubs, personal workspace first; starts a club (a verified email address is needed) |
| `GET`, `PATCH`, `DELETE /v1/orgs/{orgId}` | Reads, renames or deletes a club; a deleted club can be restored until `orgs.deleted_org_retention` ends, then the `orgs_purge` job removes it with its data |
| `/v1/orgs/{orgId}/members` | Lists members, changes a role, removes a member; `POST /v1/orgs/{orgId}/leave` |
| `POST /v1/orgs/{orgId}/invitations` | Emails an invitation with a role the inviter may give |
| `POST /v1/invitations/accept` | Joins with the token from the email, for the account whose verified address was invited |

The invitation email links to the page in the `orgs.invitation_url` runtime setting (`http://localhost:3000/invitations` until an operator sets Shelfie's web app), with a single-use token in the fragment for the page to post. Invitations, roles, ownership and deletion are described in [Organisations](../../start/organisations.md).

## 2. Generate the reading list

A club book has a title, unique in the club, an optional author, a status and a note. The command is chapter 9's `orb gen module` with `--org`:

```bash
orb gen module ClubBook title:string:unique 'author:string?' 'status:enum(proposed,reading,finished)' note:text --org
```

```text
✓ Created module clubbooks

  Module:      clubbooks (table club_books, IDs like clb_…)
  API:         /v1/orgs/{orgId}/club-books, for an organisation's club books (guard.OrgMember)
  Permissions: clubbooks.club_book.read, clubbooks.club_book.write (organisation roles owner, admin and member)
  Fields:
    title                string, 1 to 100 characters, unique
    author               string, up to 100 characters, optional
    status               one of proposed, reading, finished (default proposed)
    note                 text, up to 2000 characters
  Files:
    create internal/modules/clubbooks/module.go
    create internal/modules/clubbooks/clubbooks_test.go
    …
    create db/migrations/20260920000005_club_books.sql
    modify internal/modules/modules.gen.go

Next:
  1. go run ./cmd/api migrate (orb dev runs it)
  2. go run ./cmd/api openapi --dir api
  3. go test ./internal/modules/clubbooks/...
  4. go run ./cmd/api, sign in, find your personal workspace's ID with GET /v1/orgs, then POST /v1/orgs/{orgId}/club-books
```

`internal/modules/clubbooks` and its migration are exactly what it wrote. It never edits `main.go`: in an app whose `main.go` doesn't add `orgshttp` yet, the first next step says to add `gorbital.WithModules(orgshttp.Module(auth))`, because `gorbital.New` refuses routes with `guard.OrgMember` without it.

## 3. Organisation permissions

<!-- include examples/apps/shelfie/internal/modules/clubbooks/module.go -->

The permissions name `OrgRoles` instead of `Roles`: they are organisation permissions, which a member holds through their role in the club they act in, never through a platform role. All three roles read and write the list, as v0.1's organisation resources did. A club that wants only its admins to change the list drops `"member"` from `PermWrite`; a role of Shelfie's own, such as a `moderator`, only needs naming there, and gorbital declares it.

## 4. `guard.OrgMember`

<!-- include examples/apps/shelfie/internal/modules/clubbooks/delivery/routes.go -->

Every route has `guard.OrgMember` with the permission it needs. Before the body is read, the guard asks the organisations module about the `{orgId}` in the path:

| The caller | Answer |
|---|---|
| Not signed in | 401 `unauthenticated` |
| Not a member of the club, or the club is unknown, deleted, or its ID malformed | 404 `org_not_found`, the same for all four, so club IDs can't be probed |
| A member whose role lacks the permission, or an API key whose scopes lack it | 403 `forbidden` |
| A member whose role needs a second factor the session hasn't verified | 403 `mfa_required` |
| A member whose role grants it | The handler runs, with an actor acting in the club: its `OrgID`, the role's permissions, and database connections that carry the club |

The use case then takes the club from the path, checks it is the club the guard authorized, and passes it to the store, and audit events record it:

<!-- include examples/apps/shelfie/internal/modules/clubbooks/usecase/create_club_book.go -->

## 5. Cross-club isolation

A club's reading list is kept apart in layers, each of which would hold on its own:

| Layer | What it stops |
|---|---|
| Membership | `guard.OrgMember` refuses everyone who isn't a member of the club in the path with 404 `org_not_found`: another club doesn't exist for them |
| Every query filters on the club | A member of club B who sends club A's book ID under club B's path gets 404 `club_book_not_found`: every statement has `org_id` in its `WHERE` or its `INSERT`, whatever row-level security says |
| The table | `org_id` is `NOT NULL`; `UNIQUE (org_id, id)` lets other club tables reference only a book of their own club; titles are unique per club; purging a club deletes its books |
| Row-level security | A query that forgot its filter, once Shelfie turns it on (next section) |
| Tests | Each way in, on every route (section 7) |

The store's statements take the club as a parameter:

<!-- include examples/apps/shelfie/internal/modules/clubbooks/repository/select_club_book.go -->

and the table is led by it:

<!-- include examples/apps/shelfie/db/migrations/20260920000005_club_books.sql -->

The foreign key to `orgs` is added in a `DO` block when `orgs` exists. It always does in Shelfie, whose migrations run with `orgshttp`; the tests of the books and shelves modules build apps without organisations, and their databases get the table without the key instead of failing to migrate.

## 6. Row-level security

Row-level security makes PostgreSQL itself return only the rows of the club a connection carries, so a query that forgot its `org_id` filter still can't read or write another club's books ([Row-level security](../../guides/row-level-security.md)). The guard already sets the club on the request's connections; what turns it on is a migration with the policies.

`orb add rls` writes that migration in apps created with `orb new --preset full --tenancy multi`: it needs their `gorbital.lock`, which Shelfie, an app on `gorbital.Main`, doesn't have, so it stops with `has no gorbital.lock`. In Shelfie you add the migration yourself, under the next version and named `<version>_row_level_security.sql`, with the `DO` block of a multi-tenant app's `db/row_level_security.sql`. For every table with a `NOT NULL org_id`, `club_books` included and the organisations module's memberships and invitations aside, it runs:

```sql
ALTER TABLE club_books ENABLE ROW LEVEL SECURITY;
ALTER TABLE club_books FORCE ROW LEVEL SECURITY;
CREATE POLICY org_isolation ON club_books
    USING (org_id = current_setting('gorbital.org_id', true) OR current_setting('gorbital.rls_bypass', true) = 'on')
    WITH CHECK (org_id = current_setting('gorbital.org_id', true) OR current_setting('gorbital.rls_bypass', true) = 'on');
```

That migration covers the tables that exist when it runs. `orb gen module --org` sees a `*_row_level_security.sql` migration (or `rls: true` in `gorbital.yaml`) and puts the same three statements at the end of every later module's migration, so new club tables are covered too. Shelfie hasn't turned it on in this chapter, so `club_books`' migration has no policy.

The policies apply only to a database role that isn't a superuser and has no `BYPASSRLS`. The local Docker database's user is a superuser, so they don't apply there; the app warns at startup when row-level security is on and its role bypasses it.

## 7. Tests

`clubbooks_test.go` came with the module. Organisation members are sign-in's accounts, so it builds an app with `authhttp`, `orgshttp` and the module, and signs readers up with `gorbitaltest.App.SignUp`, which registers, verifies the address from the queued email and signs in; each account's personal workspace, from `GET /v1/orgs`, is the club the tests work in. `TestClubBooksAreProtected` sends every route of Ada's workspace as Bob (404 `org_not_found`, also for an unknown and a malformed club ID), Ada's book under Bob's workspace (404 `club_book_not_found` on get, update and delete, and an empty list), and a read-only API key's writes (403 `forbidden`); `TestUpdateAndDeleteClubBook` checks that every audit event names Ada and her workspace; `TestPurgingAnOrganisationDeletesItsClubBooks` checks the foreign key.

A club with more than one member is Shelfie's own test, in `cmd/api/clubs_test.go`, on the app `accounts_test.go` builds with Shelfie's sign-in options and `orgshttp`:

<!-- include examples/apps/shelfie/cmd/api/accounts_test.go#new-accounts-app -->

<!-- include examples/apps/shelfie/cmd/api/clubs_test.go#book-club -->

`cmd/api/operations_test.go` signs in as `gorbitaltest.User` principals, which aren't accounts and so can't be members; its app leaves out the modules with organisation permissions.

```bash
go test ./internal/modules/clubbooks/... ./cmd/api/...
```

## Next

[9. Generators](09-generators.md): how `orb gen module` writes a module like the shelves and club books, `orb routes` and `orb gen middleware`.
