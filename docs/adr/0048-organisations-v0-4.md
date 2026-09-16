# ADR-0048: Organisations (v0.4)

**Status:** Accepted (2026-09-15) · **Amends:** ADR-0023, ADR-0038 · **Amended by:** ADR-0050 (`orb add orgs` merges `base-full` into `base-full-multi` and converts data with new migrations), ADR-0053 (inviter re-checked at acceptance, permission-subset role assignment, organisation and invitation limits), ADR-0056 (organisations' own runtime settings, purged with the organisation)

The maintainer approved the five questions below as recommended (2026-09-15).

## Context

ADR-0023 chose shared-schema multi-tenancy as a creation-time choice: `org_id NOT NULL` on tenant rows, explicit `/v1/orgs/{orgId}/...` routes, personal workspaces at signup, platform roles separate from org roles (`owner`, `admin`, `member`), at least one owner, email invitations (hashed, single-use, 7 days, revocable), soft delete then purge, and isolation at four layers (HTTP, code, database, tests). It also planned `examples/full-multi` and `orb add orgs` for moving a single-tenant app to multi-tenant.

v0.2 and v0.3 prepared for this: `actor.Actor` has `OrgID`; `settings_values`, `jobs_definitions`, `audit_events`, `auth_sessions` and `auth_user_roles` have a nullable `org_id`. Authentication settled the patterns organisations should follow (ADR-0038): the generated app owns flows, tables and SQL; the library holds building blocks; permissions are read on every request.

Open questions before code: where org roles are stored, how a request becomes an org actor, what platform staff may see, how invitations are accepted safely, what personal workspaces allow, how account deletion interacts with ownership, how the two golden apps stay in step, and whether `orb add orgs` can ship before the recipe engine it depends on.

## Decision

### 1. Library and app

| Library: `gorbital.dev/modules/orgs` | Generated app: `internal/modules/orgs` |
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

- Created when the account is: registration, first Google or Apple sign-in, and `create-user`. The auth module calls `AccountHooks` that the composition root wires to the orgs module, because modules don't import each other or share transactions. Creating the workspace is idempotent (one personal workspace per user, enforced by a unique index) and runs again when the user lists their organisations, so a failure between the two steps repairs itself.
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
- **`orb new`** gains the tenancy question from ADR-0023 (default single-tenant) and `--tenancy single|multi`; multi-tenant apps are generated from `full-multi` byte for byte, as ADR-0041 does for `full-single`. The recipe (`full-single` or `full-multi`) in `gorbital.lock` records the choice.
- **`orb gen resource`** gains `--scope user|org`, defaulting to the app's tenancy; `--scope org` reproduces `full-multi`'s projects module exactly.

### 10. `orb add orgs` moves to v0.5

Changing a live single-tenant app to multi-tenant edits owned files (routes, account creation, every resource) and needs data migrations. ADR-0021 requires that to happen through recorded operations with 3-way merges, which arrive with the per-feature recipe split and `orb upgrade` in v0.5. Shipping it in v0.4 would mean a one-off patcher that ignores developers' edits. v0.4 ships both modes at creation; `orb add orgs` ships with `orb add` and `orb upgrade` in v0.5.

### Questions for the maintainer

Each has a recommendation in the sections above; approving this ADR approves them:

1. One role per member (section 2)?
2. No platform staff access to orgs' data in v0.4 (section 3)?
3. Personal workspaces don't accept invitations (section 5)?
4. Accepting an invitation requires the invited, verified email (section 6)?
5. Move `orb add orgs` to v0.5 (section 10)?

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

- ADR-0023: role storage, 404 for non-members, invitation acceptance rules, personal workspace limits and the move of `orb add orgs` to v0.5 are recorded here.
- ADR-0038: account creation creates a personal workspace in multi-tenant apps; account deletion checks ownership.
- Threat model (ADR-0029): add rows for cross-org access, invitation takeover and org enumeration.
- Roadmap v0.4 loses `orb add orgs` (moved to v0.5) and gains the drift check.
- Public API (ADR-0015): org permission and role names, error codes (`org_not_found`, `last_owner`, `sole_owner`, `already_member`), audit actions and `orgs.ID`.

## Implementation notes (2026-09-15)

- **Library** (`modules/orgs`): `ID`, `NewID`, `ParseID`, `RoleOwner`/`RoleAdmin`/`RoleMember`, `Memberships`, `RequireMember` (and `Authorize`, added 2026-09-16), `ErrOrgNotFound`, `ErrNotMember`, `Emails` with `NewMailEmails` (subject lines can't be broken by organisation names).
- **Account hooks:** the auth use cases gained `AccountHooks` (`AccountCreated`, `CheckAccountDeletion`, `AccountDeleted`) in both golden apps; `full-single` leaves them unset. The deletion check runs before the password and second factor, so a refusal doesn't use up a one-time code. `internal/app/orgs_hooks.go` turns `*SoleOwnerError` into 409 `sole_owner` with the organisation IDs in `errors`.
- **Domain layers stay standard library only:** organisation IDs are `string` in `domain/` and `orgs.ID` in the use case, repository and delivery layers; the role ranking (`canAssign`) lives in the use cases.
- **Tables:** `org_invitations.sent_at` records the last send, so resends count toward the 20-per-hour limit; the window excludes sends exactly an hour old. Invitation links put the token in the URL fragment (`#token=`), so it doesn't reach frontend server logs.
- **Additional error codes:** `invalid_org_name`, `org_version_conflict`, `personal_workspace`, `member_not_found`, `unknown_role`, `role_not_allowed`, `already_invited`, `invitation_not_found`, `invitation_for_another_email`, `too_many_invitations`, and `forbidden` / `mfa_required` for org roles.
- **Runtime settings:** `orgs.invitation_url`, `orgs.invitation_ttl` (1 to 30 days), `orgs.deleted_org_retention` (1 to 365 days).
- **Golden app:** `examples/full-multi` moves the projects migration after the orgs one (`20260916000002_projects.sql`). `internal/archtest/examples_test.go` lists the files allowed to differ from `full-single` and fails when any other file differs, or when a listed file no longer does.
- **CLI:** `orb new --tenancy single|multi` (asked after the preset when it is Full), recipe `base-full-multi` generated from `examples/full-multi` and checked byte for byte like the other presets.
- **`orb gen resource --scope org`:** one set of resource templates with an org branch, so a fix reaches both scopes; the default scope follows `tenancy` in `gorbital.yaml`. It reproduces `examples/full-multi`'s projects module byte for byte, and CI generates two more org-scoped resources into a copy of `full-multi` and runs their tests. Org-scoped resources declare `<module>.<resource>.read` and `.write` as a `resourcePermissions` value in their `module_<names>.go`, and the generator adds one line at `//orb:anchor org-permissions` in `permissions.go` (ADR-0021: a new file plus one line at one anchor), so every organisation role gets them without editing the role declarations. `personalWorkspace`, used by org-scoped end-to-end tests, lives in `internal/app/app_test.go` so each generated resource can use it.
- **Threat model:** rows 25 (invitation takeover) and 26 (organisation enumeration) added; row 17 done for generated multi-tenant apps.
- **Review (2026-09-15):** the purge deletes an organisation only while `deleted_at` is set and `purge_after` has passed, and records `orgs.org.purged` only for rows it removed, so one restored and deleted again between listing and deleting stays until its new purge time. `RequireMember` checks `ErrNotMember` with `errors.Is`, so an app's `Memberships` may wrap it. Checked and kept: concurrent role changes, removals and leaves lock the organisation row, so the last owner can't be removed by two requests at once; accepting, resending and revoking lock the invitation row; account deletion is a soft delete, so memberships are still there when the hook runs.

## Security review fixes (2026-09-16)

The internal security review before 1.0 found no cross-organisation read or write. It found seven issues with privilege boundaries inside one organisation, abuse limits and lifecycle edge cases (ORG-1 to ORG-7). All seven are fixed in `examples/full-multi` (so in `base-full-multi` and `orb add orgs`), with one additive function in `modules/orgs`. `examples/full-single` has no organisations and is unchanged.

| ID | Finding | Fix |
|---|---|---|
| ORG-1 | A pending invitation kept its role after the inviter was removed or demoted, so a removed admin or owner could get back in | Accepting checks, under the organisation's lock, that the inviter (`invited_by`) still has a live account and is a member whose role holds `orgs.members.manage` and may assign the invitation's role; otherwise 404 `invitation_not_found`. Resending makes the resender the inviter (the email already named them), so a manager can vouch for an invitation whose sender left. Revoking stays limited to roles the caller may assign: owners revoke any invitation, admins those for admins and members |
| ORG-2 | `canAssign` ranked every role an app adds as `member`, so an admin could give themselves a custom role that may delete the organisation | Assignment compares permissions: a member may give, change or remove a role only when its permissions are a subset of their own role's, and only owners manage owners. No rank table to keep in step with `declareOrgPermissions` |
| ORG-3 | No limit on creating organisations, so the per-organisation invitation limit didn't bound email volume, and a purge backlog could grow | Creating an organisation and sending or resending invitations need a verified email address (403 `email_not_verified`, the auth module's code). New settings `orgs.max_owned` (20; live organisations a user owns, personal workspace aside, checked on create and restore under a per-user advisory lock; 409 `too_many_orgs`) and `orgs.user_invitations_per_hour` (50; a `ratelimitpg` limiter `orgs_invitations` keyed by user, on top of 20 per organisation; 429 `too_many_invitations`). Both require a reason. `Purge` works in batches of 100 until none are left or 5 minutes have passed, well within the job's 10-minute timeout |
| ORG-4 | The caller's role was read by `RequireMember` before the transaction and trusted after the organisation lock | Every membership, invitation, rename and delete use case locks the organisation, then calls `orgs.RequireMember` again on the transaction and uses that role. A request that passed the first check just before a removal or demotion committed gets 404 or 403 |
| ORG-5 | `Restore` checked `catalog.Permissions`, skipping the org catalog's `RequireMFA` step-up | New `orgs.Authorize(ctx, catalog, member, permission)` in `modules/orgs`: the permission half of `RequireMember`, which now calls it, for a membership read another way. `Restore` uses it, so a role that requires two-factor authentication gets 403 `mfa_required` without it |
| ORG-6 | A deleted account stayed a member, maybe the only live-looking owner, of organisations that were soft deleted at the time | `RemoveAccount` removes the membership from every organisation, deleted ones included (no hand-over there), and re-reads the role under the lock. `CountOwners` (now "other owners than this user"), the owner and member counts of the sole-owner check, and the hand-over candidate ignore deleted accounts, so an owner with a deleted account left from before can be removed and doesn't satisfy "at least one owner" |
| ORG-7 | Accept locked the invitation then the organisation; resend the opposite, so the two could deadlock | Accept finds the invitation by token hash without a lock, locks the organisation, then locks the invitation and checks the token hash again. Every invitation change now locks organisation, then invitation |

- **Why these shapes:** re-checking the inviter at acceptance, rather than revoking invitations when members change, covers every way a member loses rights (removal, demotion, leaving, account deletion, the account purge's cascade, an app's own paths) with one check, and keeps history intact. Comparing permission sets needs no configuration and stays right when apps add roles. `orgs.max_owned` counts live organisations, so deleting one frees a slot for a new one without letting restores exceed it; email volume is bounded separately by the per-user limiter.
- **Store port changes** (app-owned code): `CountOwners(ctx, orgID, excludeUserID)`, `SelectInvitationByTokenHash(ctx, hash, lock)`, `ReplaceInvitationToken(ctx, id, hash, invitedBy, now, expiresAt)`, new `LockUserOrgs` and `CountOwnedOrgs`. No migration: the tables are unchanged.
- **Public surface:** error code `too_many_orgs` added (`email_not_verified` already existed); settings `orgs.max_owned` and `orgs.user_invitations_per_hour`; no audit action, permission or job name changed. `orgs.member.removed` for a deleted account now carries the role in its metadata. `modules/orgs` gains `Authorize` (additive, `api/modules-orgs.txt`).
- **Regression tests** (`internal/modules/orgs/usecase/review_test.go`): `TestInvitationsEndWithTheInvitersRole`, `TestCustomRolesCantExceedTheAssigner`, `TestOrganisationAndInvitationLimits`, `TestOwnedOrganisationLimitHoldsUnderConcurrency`, `TestPurgeWorksThroughABacklog`, `TestRoleIsCheckedAgainUnderTheLock` (a store that runs a removal or demotion between the first role read and the transaction), `TestRestoreNeedsTheSameStepUpAsDelete`, `TestDeletedAccountsDontStayOwners`, `TestInvitationChangesLockTheOrganisationFirst` (records lock order); `modules/orgs` `TestAuthorize`. Each was checked to fail with its fix reverted.

| Check | Result |
|---|---|
| `modules/orgs`: gofmt, vet, `go test ./...` | pass |
| root: gofmt, vet, `go test ./...` (archtest, golden apps don't drift) | pass |
| `examples/full-multi`: gofmt, vet, `go test ./...` with PostgreSQL and Mailpit | pass |
| Regression tests with each fix reverted | fail as expected |
| `cli`: `go generate ./internal/recipes/`, `go test ./...` | pass |
| `cli`: `ORB_E2E=1 go test ./internal/cli -run TestAddOrgsConvertsADatabase` | pass |
| `go run -C internal/tools/apicheck .` | pass (13 modules match `api/`) |
