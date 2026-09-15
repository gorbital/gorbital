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

Start it with `orb dev`. Seed data creates `admin@example.com` with a personal workspace holding three example projects, so `GET /v1/orgs` and `GET /v1/orgs/{orgId}/projects` return data straight away.

## Roles

Every member has exactly one role.

| Role | Can |
|---|---|
| `owner` | Everything, including deleting the organisation and managing owners |
| `admin` | Rename the organisation, invite, change and remove members and admins, work with resources |
| `member` | See the organisation and its members, work with resources |

- Nobody gives, changes or removes a role above their own, and only owners manage owners.
- The last owner can't leave, be demoted or be removed (409 `last_owner`). Promote another member to owner first.
- Add roles for your product, such as a read-only viewer, in `declareOrgPermissions` in `internal/app/permissions.go`.

## Personal workspaces

Every account gets a workspace called "Personal" when it is created: at registration, at the first Google or Apple sign-in, and with `create-user`. The account owns it. A personal workspace can't be left, deleted on its own or shared by invitation; teams create an organisation instead. It is deleted with the account.

## Invitations

<div class="steps">

1. **Invite**

   An owner or admin calls `POST /v1/orgs/{orgId}/invitations` with an email address and a role no higher than their own. The email links to the page in the `orgs.invitation_url` runtime setting, with a single-use token in the URL fragment. Only the token's hash is stored. An organisation can send 20 invitations an hour, resends included.

2. **Accept**

   The invited person signs in, or registers, with the invited address and verifies it, then your frontend calls `POST /v1/invitations/accept` with the token. An account with a different email address can't use the link, even if it was forwarded.

3. **Resend or revoke**

   Resending replaces the token and the expiry, so the old link stops working. Revoking ends the invitation.

</div>

## Add an org-scoped resource

In a multi-tenant app, `orb gen resource` scopes resources to organisations by default:

```bash
orb gen resource Invoice number:string:unique 'status:enum(draft,sent,paid)'
```

It writes endpoints under `/v1/orgs/{orgId}/invoices`, declares `invoices.invoice.read` and `invoices.invoice.write`, and adds one line at `//orb:anchor org-permissions` so every organisation role holds them. Change which roles hold them in `declareOrgPermissions`.

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

## Deleting organisations and accounts

- An owner deletes an organisation with `DELETE /v1/orgs/{orgId}`. Members lose access at once. An owner can restore it with `POST /v1/orgs/{orgId}/restore` until the `orgs.deleted_org_retention` period ends; then the `orgs_purge` job removes it with every org-scoped row.
- Deleting an account is refused with 409 `sole_owner` while the account is the only owner of an organisation with other members. The response lists those organisations. Organisations where the account is the only member are deleted with it.

## Runtime settings

| Setting | Default | Allowed |
|---|---|---|
| `orgs.invitation_url` | none | The frontend page invitation links open |
| `orgs.invitation_ttl` | 7 days | 1 to 30 days |
| `orgs.deleted_org_retention` | 30 days | 1 to 365 days |

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
| `too_many_invitations` | 429 | More than 20 invitations from one organisation in an hour |
