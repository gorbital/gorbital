# ADR-0048: Organisations (v0.4)

**Status:** Accepted (2026-09-15) · **Amends:** ADR-0023, ADR-0038

The maintainer approved the five questions below as recommended (2026-09-15).

## Context

ADR-0023 chose shared-schema multi-tenancy as a creation-time choice: `org_id NOT NULL` on tenant rows, explicit `/v1/orgs/{orgId}/...` routes, personal workspaces at signup, platform roles separate from org roles (`owner`, `admin`, `member`), at least one owner, email invitations (hashed, single-use, 7 days, revocable), soft delete then purge, and isolation at four layers (HTTP, code, database, tests). It also planned `examples/full-multi` and `aps add orgs` for moving a single-tenant app to multi-tenant.

v0.2 and v0.3 prepared for this: `actor.Actor` has `OrgID`; `settings_values`, `jobs_definitions`, `audit_events`, `auth_sessions` and `auth_user_roles` have a nullable `org_id`. Authentication settled the patterns organisations should follow (ADR-0038): the generated app owns flows, tables and SQL; the library holds building blocks; permissions are read on every request.

Open questions before code: where org roles are stored, how a request becomes an org actor, what platform staff may see, how invitations are accepted safely, what personal workspaces allow, how account deletion interacts with ownership, how the two golden apps stay in step, and whether `aps add orgs` can ship before the recipe engine it depends on.

## Decision

### 1. Library and app

| Library: `apistock.dev/modules/orgs` | Generated app: `internal/modules/orgs` |
|---|---|
| `orgs.ID` (a distinct string type, prefix `org_`) so a user ID can't be passed where an org ID is expected | Domain rules: names, roles, last owner, personal workspace limits |
| `RequireMember(permission)` HTTP middleware over a `Memberships` interface the app implements | Use cases: create, rename, list, members, invitations, transfer, leave, delete, restore, purge |
| Org role catalog: an `auth.Catalog` instance for org roles, with the same deny-by-default rules | Repository: one SQL file per operation (ADR-0032) |
| Invitation tokens (random, stored as SHA-256) and invitation email content | Delivery: `/v1/orgs` endpoints, audit events, the purge job |

### 2. Tables

| Table | Columns (abridged) |
|---|---|
| `orgs` | `id` (`org_`), `name`, `personal`, `created_by`, `created_at`, `updated_at`, `deleted_at`, `purge_after`, `version` |
| `org_members` | `org_id`, `user_id`, `role`, `joined_at`, `added_by`; primary key `(org_id, user_id)` |
| `org_invitations` | `id` (`inv_`), `org_id`, `email`, `role`, `token_hash`, `invited_by`, `created_at`, `expires_at`, `accepted_at`, `revoked_at`; one pending invitation per `(org_id, lower(email))` |

- **One role per member.** Org roles live in `org_members.role`, checked against the org catalog. Multiple roles per member add little for owner, admin and member and complicate "at least one owner".
- `auth_user_roles.org_id` stays `NULL` (platform roles only) and `auth_sessions.org_id` stays unused: routes name the org, so sessions don't bind to one. Both columns remain reserved rather than dropped, so no v0.2 app needs a migration.

### 3. Requests

- Org routes are `/v1/orgs/{orgId}/...`. `RequireMember` loads the caller's membership in one query, like roles on every request, so removal applies immediately.
- `RequireMember` is a function every organisation use case calls first, `orgs.RequireMember(ctx, memberships, catalog, orgID, permission)`, not `net/http` middleware: the `{orgId}` path value is only known after routing, and a check in the use case also protects jobs and commands that call it. The generated denial tests (section 8) verify every operation makes the call.
- Not a member, or the org is deleted: **404 `org_not_found`**, the same as an org that doesn't exist, so IDs can't be probed. A member without the permission: **403 `forbidden`**.
- The middleware sets the actor's `OrgID` and replaces `Permissions` with the member's org-role permissions for that request. `/ops/*` never passes through it, so platform and org permissions never mix.
- Platform staff have **no implicit access** to organisations' data. Support access (time-limited, audited, visible to the org) is a later feature.
- Org roles don't require a second factor by default; the org catalog supports `RequireMFA` for apps that want it. A per-org "members must use 2FA" policy needs per-org settings (v1.1).
- `org_id` is added to audit events (from the actor), job metadata and log attributes.

### 4. Roles

| Role | Can |
|---|---|
| `owner` | Everything, including delete, transfer and managing owners |
| `admin` | Rename, invite and remove members and admins, manage resources |
| `member` | Use org-scoped resources |

- An admin can't invite, promote or remove owners.
- The last owner can't leave, be demoted or removed: **409 `last_owner`**. Ownership moves by promoting another member to owner.
- Permissions follow `module.resource.action` (for example `orgs.members.invite`, `projects.project.write`); apps add roles from the catalog.

### 5. Personal workspaces

- Created in the same transaction as the account: registration, first Google or Apple sign-in, and `create-user`.
- Named "Personal", `personal = true`, with the user as owner.
- Can't be left, transferred or deleted on its own, and **doesn't accept invitations**. Teams create an organisation. This keeps "personal" meaning personal and avoids a workspace turning into a shared org with confusing ownership.
- Deleted with the account.

### 6. Invitations

- Owners and admins invite by email with a role no higher than their own; 20 invitations per org per hour.
- The email links to the frontend invitation URL (a runtime setting) with a 32-byte token; only its hash is stored. Expiry is a runtime setting: 7 days by default, 1 to 30 allowed.
- **Accepting requires a signed-in user whose verified email matches the invitation** (case-insensitive). A forwarded link can't be used by someone else. New users register (or sign in with Google or Apple), verify, then accept.
- Resending replaces the token; revoking or accepting ends it. Accepting an invitation for an org the user already belongs to returns 409 `already_member`.

### 7. Lifecycle and account deletion

- An owner deletes an org: `deleted_at` is set, it disappears for members, and `purge_after` is set 30 days out (a runtime setting). Owners can restore it before then.
- The `orgs_purge` job deletes purged orgs; org-scoped tables cascade through composite foreign keys. Audit events keep their `org_id` (no foreign key) as history.
- Deleting an account is refused with **409 `sole_owner`** while the user is the only owner of an org with other members; the response lists those orgs. Orgs where the user is the only member are soft deleted with the account.
- Every change is audited: `orgs.org.created`, `.renamed`, `.deleted`, `.restored`, `.purged`, `orgs.member.added`, `.role_changed`, `.removed`, `.left`, `orgs.invitation.created`, `.resent`, `.revoked`, `.accepted`.

### 8. Org-scoped resources and isolation

| Layer | Generated as |
|---|---|
| HTTP | Routes under `/v1/orgs/{orgId}/<resources>` behind `RequireMember` with the resource's permissions |
| Code | Repository methods take `orgs.ID` first; there's no method that reads an org-scoped row without one |
| Database | `org_id NOT NULL REFERENCES orgs (id) ON DELETE CASCADE`, `UNIQUE (org_id, id)` so other tables can reference `(org_id, id)`, uniqueness per org (for example `(org_id, lower(name))`), and composite foreign keys that include `org_id` |
| Tests | For every org-scoped resource: a member of another org gets 404 on read, update, delete and list, and can't reference the row from their own org |

Rows keep `created_by` (the user) for display and audit; access comes only from membership.

### 9. Generation

- **`examples/full-multi`** is a golden app: `full-single` plus the orgs module, personal workspaces, and `projects` scoped to organisations.
- **Drift check:** a test lists the files allowed to differ between `full-single` and `full-multi`; every other file must be identical, so the two apps can't drift apart.
- **`aps new`** gains the tenancy question from ADR-0023 (default single-tenant) and `--tenancy single|multi`; multi-tenant apps are generated from `full-multi` byte for byte, as ADR-0041 does for `full-single`. The recipe (`full-single` or `full-multi`) in `apistock.lock` records the choice.
- **`aps gen resource`** gains `--scope user|org`, defaulting to the app's tenancy; `--scope org` reproduces `full-multi`'s projects module exactly.

### 10. `aps add orgs` moves to v0.5

Changing a live single-tenant app to multi-tenant edits owned files (routes, account creation, every resource) and needs data migrations. ADR-0021 requires that to happen through recorded operations with 3-way merges, which arrive with the per-feature recipe split and `aps upgrade` in v0.5. Shipping it in v0.4 would mean a one-off patcher that ignores developers' edits. v0.4 ships both modes at creation; `aps add orgs` ships with `aps add` and `aps upgrade` in v0.5.

### Questions for the maintainer

Each has a recommendation in the sections above; approving this ADR approves them:

1. One role per member (section 2)?
2. No platform staff access to orgs' data in v0.4 (section 3)?
3. Personal workspaces don't accept invitations (section 5)?
4. Accepting an invitation requires the invited, verified email (section 6)?
5. Move `aps add orgs` to v0.5 (section 10)?

## Why

- Following ADR-0038's split keeps organisations readable, owned by the app and fixable with `go get` where it matters (tokens, middleware, catalog).
- 404 for non-members, a distinct `orgs.ID` type, composite foreign keys and generated denial tests mean one mistake in one layer doesn't leak data.
- Matching the invited email closes the most common invitation takeover; refusing invites to personal workspaces keeps ownership simple.
- Two golden apps with a drift check keep both modes tested without letting them diverge.

## Trade-offs

- Every org request costs a membership query, on top of the session and roles.
- One role per member means "admin plus billing" needs a custom role rather than two roles.
- Invited people with a different email address must be invited again at the right address.
- A second golden app and its recipe templates add generation time and review surface.
- Single-tenant apps can't become multi-tenant with a command until v0.5.

## Consequences

- ADR-0023: role storage, 404 for non-members, invitation acceptance rules, personal workspace limits and the move of `aps add orgs` to v0.5 are recorded here.
- ADR-0038: account creation creates a personal workspace in multi-tenant apps; account deletion checks ownership.
- Threat model (ADR-0029): add rows for cross-org access, invitation takeover and org enumeration.
- Roadmap v0.4 loses `aps add orgs` (moved to v0.5) and gains the drift check.
- Public API (ADR-0015): org permission and role names, error codes (`org_not_found`, `last_owner`, `sole_owner`, `already_member`), audit actions and `orgs.ID`.
