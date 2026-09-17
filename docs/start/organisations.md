# Organisations

In a multi-tenant app, data belongs to organisations. People join them as members with one role each, invite others by email, and reach an organisation's rows only while they are members. Decision: [ADR-0048](../adr/0048-organisations-v0-4.md).

> [!NOTE]
> Tenancy is chosen when you create the app. To turn an existing single-tenant app multi-tenant, run `orb add orgs`: it merges the multi-tenant files into yours on a branch and adds migrations that give every account a personal workspace and move projects into it. See [Upgrading apps](upgrading.md).

## Create a multi-tenant app

<div class="code-group">

```bash terminal
orb new acme-api --preset full --tenancy multi
```

```text output
creating acme-api in ./acme-api
preset full · tenancy multi · library ../gorbital

✓ ran go mod tidy
✓ initialised git

created acme-api
```

</div>

Start it with `orb dev`. Seed data creates `admin@example.com`; its personal workspace is created the first time it calls `GET /v1/orgs`, and the example `projects` module serves `GET /v1/orgs/{orgId}/projects`. (An app created by orb v0.1 seeds three example projects in the workspace.)

## In an app on gorbital.Main

Organisations are one line of `main.go`, with the app's sign-in passed to them. The code is in the library, `gorbital.dev/gorbital/orgshttp` ([Methods](../methods/gorbital-orgshttp.md)), not in your app:

```go cmd/api/main.go
func main() {
	auth := authhttp.New()
	gorbital.Main(
		gorbital.WithAuth(auth),
		gorbital.WithModules(opshttp.Module(), orgshttp.Module(auth)), // organisations need the app's sign-in
		gorbital.WithModules(modules.All()...),
		gorbital.WithMigrations(migrations.FS),
	)
}
```

It is full-multi's `internal/modules/orgs` moved into the library, with v0.1's paths under `/v1/orgs` and `/v1/invitations`, operation IDs, schemas, error codes, audit actions, permissions and roles, the `orgs.*` settings, the `orgs_purge` job, and its two migrations under the versions v0.1 apps hold them under, so a database migrated by a v0.1 multi-tenant app migrates as a no-op. Contract tests compare the whole app's OpenAPI and public names with full-multi's frozen v0.1.0 contract. `orgshttp.Module(auth)` also:

- gives every new account its personal workspace, and handles account deletion (409 `sole_owner`), through `authhttp`;
- serves organisations' service accounts under `/v1/orgs/{orgId}/service-accounts`, which sign-in stores and authenticates;
- lists `deleted_organisations` in `/ops/retention` and the `orgs_invitations` limiter in `/ops/auth/rate-limits`.

`gorbital.New` refuses to start when `auth` isn't the authenticator given to `WithAuth`. Invitation emails carry the app's name and `APP_PUBLIC_URL`; pass the same `mail.Brand` to `authhttp.Brand` and `orgshttp.Brand` to change both.

### Organisation-scoped modules

Your modules scope routes to an organisation with [`guard.OrgMember`](../methods/gorbital-guard.md#OrgMember) and declare organisation permissions with `OrgRoles`. `orb gen module --org` writes such a module ([Generating code](../guides/generating-code.md)):

```go internal/modules/invoices/module.go
Permissions: []gorbital.Permission{
	{Name: usecase.PermRead, Description: "See invoices", OrgRoles: []string{"owner", "admin", "member"}},
	{Name: usecase.PermWrite, Description: "Create, change and delete invoices", OrgRoles: []string{"owner", "admin"}},
},
```

```go internal/modules/invoices/delivery/routes.go
invoices := r.Group("/v1/orgs/{orgId}/invoices", gorbital.Tags("Invoices"))
gorbital.Get(invoices, "/{id}", h.getInvoice, guard.OrgMember(usecase.PermRead))
gorbital.Post(invoices, "", h.createInvoice, guard.OrgMember(usecase.PermWrite))
```

The guard runs before the request's body is read. It asks organisations whether the caller is a member of `{orgId}` whose role grants the permission, on every request, with the semantics of `orgs.RequireMember`:

| Caller | Answer |
|---|---|
| Not a member, or the organisation is deleted, doesn't exist or has a malformed ID | 404 `org_not_found`, the same for all four |
| A member whose role doesn't grant the permission | 403 `forbidden` |
| A member whose role grants it only with a second factor, without one or with an API key | 403 `mfa_required` |
| A member's API key | The role's permissions, limited to the key's scopes |
| A service account of the organisation, with its key | Its role's permissions, limited to the key's scopes; never another organisation's routes |

Then the actor acts in the organisation: `actor.Actor.OrgID` is set and its permissions are the role's, so audit events record the organisation, and the request's database connections carry it (`postgres.WithOrg`) for [row-level security](../guides/row-level-security.md#in-an-app-on-gorbitalmain). Platform roles grant nothing inside an organisation. Registration fails for a route without `{orgId}` in its path or with `guard.Public()`, and `gorbital.New` fails when routes use the guard and the app has no `orgshttp.Module`.

A permission with `OrgRoles` is declared in the organisation catalog, not the platform's: no platform role holds it, and the `roles` command doesn't list it. A role name you use that isn't `owner`, `admin` or `member` becomes an organisation role, such as `billing`; nobody gives it unless their own role holds every permission it grants, as in v0.1.

The guard replaces the `orgs.RequireMember` call at the start of each use case in v0.1's generated resources. Keep `org_id` in every query anyway: the guard checks who may act in the organisation, the query decides which rows belong to it.

In tests, `gorbitaltest.App.SignUp` creates real accounts, whose personal workspaces are organisations ([Testing with gorbitaltest](../guides/testing-with-gorbitaltest.md)):

```go
auth := authhttp.New()
app := gorbitaltest.NewWithEnv(t, map[string]string{"AUTH_ENCRYPTION_KEYS": authlib.NewKeyringKey("test")},
	gorbital.WithAuth(auth), gorbital.WithModules(orgshttp.Module(auth), invoices.Module()), gorbital.WithMigrations(migrations.FS))
ada, _ := app.SignUp(t, "ada@example.com")
bob, _ := app.SignUp(t, "bob@example.com")
// ada's personal workspace from GET /v1/orgs, then:
bob.Get("/v1/orgs/" + adaWorkspace + "/invoices").AssertProblem(t, http.StatusNotFound, "org_not_found")
```

What differs from a v0.1 app:

- A request without credentials to an organisation operation gets 401 before its body is parsed, where v0.1 answered 422 to an invalid body first.
- The organisation settings operations answer `setting_not_found`, `setting_version_conflict`, `setting_reason_required` and `invalid_setting_value` themselves, so they work without `opshttp`.
- The dev console doesn't preview the invitation email yet.
- There is no `seed` command: create data through the API, or with SQL in your own command.
- The organisations module's migrations have v0.1's versions (`20260916000001`, `20260918000002`). A database already migrated past them can't apply them later: add `orgshttp` before your first migration dated after them, or start from a new database.

## Roles

Every member has exactly one role.

| Role | Can |
|---|---|
| `owner` | Everything, including deleting the organisation and managing owners |
| `admin` | Rename the organisation, change its settings, invite, change and remove members and admins, work with resources |
| `member` | See the organisation, its members and its settings, work with resources |

- Nobody gives, changes or removes a role that grants a permission their own role doesn't, and only owners manage owners. Roles are compared by their permissions, not their names, so a role you add can't be used to climb: an admin can't give anyone a role that may delete the organisation.
- The last owner can't leave, be demoted or be removed (409 `last_owner`). Promote another member to owner first.
- Add roles for your product, such as a read-only viewer, in `declareOrgPermissions` in `internal/app/permissions.go`, or in an app on `gorbital.Main` by naming them in a permission's `OrgRoles`.

## Personal workspaces

Every account gets a workspace called "Personal" when it is created: at registration, at the first Google or Apple sign-in, and with `create-user`. The account owns it. A personal workspace can't be left, deleted on its own or shared by invitation; teams create an organisation instead. It is deleted with the account.

## Invitations

<div class="steps">

1. **Invite**

   An owner or admin with a verified email address calls `POST /v1/orgs/{orgId}/invitations` with an email address and a role they could give. The email links to the page in the `orgs.invitation_url` runtime setting, with a single-use token in the URL fragment. Only the token's hash is stored. An organisation can send 20 invitations an hour, and a user `orgs.user_invitations_per_hour` across all their organisations, resends included.

2. **Accept**

   The invited person signs in, or registers, with the invited address and verifies it, then your frontend calls `POST /v1/invitations/accept` with the token. An account with a different email address can't use the link, even if it was forwarded. The link works only while whoever sent it is still a member who may give the role: removing or demoting them, or deleting their account, ends their invitations (404 `invitation_not_found`).

3. **Resend or revoke**

   Resending replaces the token and the expiry, so the old link stops working, and makes you the invitation's sender. Revoking ends the invitation. Both need a role that could give the invitation's role: owners handle every invitation, admins those for admins and members.

</div>

## Add an org-scoped module

In an app on `gorbital.Main`, `orb gen module --org` writes a module whose records belong to organisations (`orb gen resource` does the same without the flag in a multi-tenant app):

```bash
orb gen module Invoice number:string:unique 'status:enum(draft,sent,paid)' --org
```

It writes endpoints under `/v1/orgs/{orgId}/invoices`, each guarded by `guard.OrgMember`, and declares `invoices.invoice.read` and `invoices.invoice.write` with `OrgRoles` owner, admin and member in the module's `module.go`; change which roles hold them there.

In an app on the v0.1 layout, `orb gen resource` scopes resources to organisations by default, and adds one line at `//orb:anchor org-permissions` in `internal/app/permissions.go` so every organisation role holds them. The rest of this section shows that layout.

## Check membership in a use case

Every organisation operation starts by calling `orgs.RequireMember`. It is a function rather than middleware: the organisation ID is only known after routing, and a check inside the use case also protects jobs and commands that call it.

```go internal/modules/invoices/usecase/invoices.go
func (s *Service) Get(ctx context.Context, orgID orgs.ID, id string) (Invoice, error) {
	ctx, _, err := orgs.RequireMember(ctx, s.memberships, s.catalog, orgID, PermRead)
	if err != nil {
		return Invoice{}, err // 404 org_not_found, or 403 forbidden
	}
	return s.store.SelectInvoice(ctx, orgID, id)
}
```

The membership is read on every call, so removing a member takes effect on their next request. Someone who isn't a member gets the same 404 as for an organisation that doesn't exist, so organisation IDs can't be probed.

> [!DONT]
> Don't write a repository query for an org-scoped table without `org_id` in its `WHERE` clause. It is the one layer the compiler can't check for you.

## Four layers of isolation

One mistake in one layer shouldn't leak data. Each layer is checked by a test in your app.

| Layer | What it does | Proved by |
|---|---|---|
| HTTP | Routes live under `/v1/orgs/{orgId}`. Non-members get 404 `org_not_found`. | `TestProjectsEndToEnd` |
| Code | Every use case calls `orgs.RequireMember` first, and every query filters on `org_id`. | `TestRequiresMembership` |
| Database | `org_id NOT NULL` with a cascading foreign key, and `UNIQUE (org_id, id)` for references between org-scoped tables. | `TestPurgingAnOrganisationDeletesItsProjects` |
| Tests | A member of another organisation gets 404 on read, update, delete and list. | `TestOrganisationsCantReachEachOthersProjects` |

### A fifth layer: row-level security

Every database connection already carries the organisation `orgs.RequireMember` checked. Run `orb add rls` and PostgreSQL enforces it too: a query that forgets `org_id` sees only that organisation's rows, and a write can't reach another organisation. It adds one migration and changes no code. The app's database role must not be a superuser or have `BYPASSRLS`. See [Row-level security](../guides/row-level-security.md).

```bash
orb add rls
go run ./cmd/migrate
```

## Deleting organisations and accounts

- Creating an organisation needs a verified email address, and a user owns at most `orgs.max_owned` organisations besides their personal workspace (409 `too_many_orgs`). Deleted organisations don't count until they are restored.
- An owner deletes an organisation with `DELETE /v1/orgs/{orgId}`. Members lose access at once. An owner can restore it with `POST /v1/orgs/{orgId}/restore` until the `orgs.deleted_org_retention` period ends; then the `orgs_purge` job removes it with every org-scoped row. Restoring needs the same role, and the same second factor if the role requires one, as deleting.
- Deleting an account is refused with 409 `sole_owner` while the account is the only owner of an organisation with other members. The response lists those organisations. Organisations where the account is the only member are deleted with it, and the account stops being a member everywhere, deleted organisations included. Owners whose accounts are deleted don't count toward "at least one owner".

## Organisation settings

Some runtime settings let each organisation choose its own value ([ADR-0056](../adr/0056-per-organisation-settings.md)). The app ships one, `orgs.invitation_ttl`: an owner or admin can make their organisation's invitation links last 2 days while the platform keeps 7.

```bash
curl -X PUT http://127.0.0.1:8080/v1/orgs/$ORG/settings/orgs.invitation_ttl   -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json'   -d '{"value":"48h","version":0,"reason":"links for our event"}'
```

Members list them with `GET /v1/orgs/{orgId}/settings`, and see changes with `.../settings/{key}/history`. `DELETE` on the setting goes back to the platform value. Values stay within the platform's bounds, and a reason is required where the platform requires one. To let organisations set one of your own settings, add `settings.OrgOverridable()` to its declaration in `internal/app/settings.go`; read it after `orgs.RequireMember` so `Get(ctx)` sees the organisation. See [Runtime settings](../guides/runtime-settings.md#per-organisation-settings).

## Runtime settings

| Setting | Default | Allowed |
|---|---|---|
| `orgs.invitation_url` | none | The frontend page invitation links open |
| `orgs.invitation_ttl` | 7 days | 1 to 30 days; each organisation may set its own |
| `orgs.deleted_org_retention` | 30 days | 1 to 365 days |
| `orgs.max_owned` | 20 | 1 to 10,000 organisations a user may own, personal workspace aside |
| `orgs.user_invitations_per_hour` | 50 | 1 to 10,000 invitations a user may send or resend an hour, shared across instances |

Changing any of them needs a reason, kept in the setting's history: the invitation page receives every invitation token.

## Error codes

| Code | Status | When |
|---|---|---|
| `org_not_found` | 404 | You aren't a member, or the organisation is deleted or doesn't exist |
| `forbidden` | 403 | Your role doesn't allow this |
| `role_not_allowed` | 403 | Giving, changing or removing a role above your own, or an owner when you aren't one |
| `last_owner` | 409 | The only owner leaving, or being demoted or removed |
| `sole_owner` | 409 | Deleting an account that is the only owner of an organisation with other members |
| `personal_workspace` | 409 | Leaving, deleting or inviting into a personal workspace |
| `already_member` | 409 | Inviting or accepting for someone who is already a member |
| `already_invited` | 409 | The address already has an open invitation; resend it instead |
| `invitation_not_found` | 404 | The invitation doesn't exist, was used or revoked, or expired |
| `invitation_for_another_email` | 403 | Accepting with an account whose verified address isn't the invited one |
| `too_many_invitations` | 429 | More than 20 invitations from one organisation, or `orgs.user_invitations_per_hour` from one user, in an hour |
| `too_many_orgs` | 409 | Creating or restoring an organisation when you own `orgs.max_owned` already |
| `email_not_verified` | 403 | Creating an organisation or sending an invitation before verifying your email address |
