# ADR-0058: API keys and service accounts

**Status:** Accepted (2026-09-16) · **Amends:** ADR-0024, ADR-0038, ADR-0039, ADR-0043, ADR-0048

## Context

v1.1 (roadmap item 3) adds API keys and service accounts: programs calling a Full app without a person's session. ADR-0024 listed "GitHub login, API keys" for v1.1 without a design. What exists:

| Area | Today | Evidence |
|---|---|---|
| Authentication | `auth.Middleware(Authenticator)` resolves a bearer token or the `__Host-session` cookie into a `Principal` and a user actor; invalid tokens continue anonymously, store failures answer 503 | `modules/auth/middleware.go` |
| Tokens | Session tokens are 32 random bytes, base64url, stored as SHA-256; lookup is by hash | `modules/auth/token.go`, `auth_sessions.token_hash` |
| Actors | `actor.KindService` exists but nothing sets it; permissions and `StepUp` live on the actor | `actor/actor.go` |
| Permissions | Read from roles on every request; `Catalog.PermissionsFor(roles, mfaVerified)` withholds roles in `RequireMFA` from sessions without a second factor | ADR-0038, ADR-0043, `catalog_mfa.go` |
| Ops | Every `/ops` permission belongs to `platform_admin` or `ops_viewer`, both `RequireMFA` | `internal/app/permissions.go` |
| Account management | Use cases take the session from `requirePrincipal`; password, 2FA, passkey and identity changes use `confirmUser` and the `auth.reauth_attempts` budget | `internal/modules/auth/usecase/{mfa,passkeys}.go` |
| Organisations | `orgs.RequireMember` and `orgs.Authorize` accept only `actor.KindUser` and replace the actor's permissions with the member's org role | `modules/orgs/member.go`, ADR-0048 |
| Rate limits | Shared GCRA limiters in PostgreSQL keyed by client network (`ratelimit.ClientKey`) | ADR-0052 |
| Account lifecycle | Password reset and claiming an address end every session; account deletion ends sessions; `auth_cleanup` purges | ADR-0038 security review fixes |

Open questions: the key format and how it is found and compared; how keys reach the middleware without ever becoming sessions; where service accounts live, and how an organisation's service account passes `orgs.RequireMember` without the auth module importing organisations; what scopes mean; what happens to roles requiring two-factor authentication; which operations a key may never perform; what revokes keys; and how keys stay out of logs.

## Options

### Key format and storage

| | 1. Random token, looked up by hash (like sessions) | 2. `gbk_<lookup>_<secret>`, lookup ID indexed, SHA-256 of the key compared in constant time | 3. Signed tokens (JWT, PASETO) |
|---|---|---|---|
| Recognisable | No: indistinguishable from a session token | Prefix for middleware, secret scanners and people | Prefix possible |
| Loggable handle | None | The lookup ID | Claims, but self-contained tokens can't be revoked without a lookup |
| Revocation | Immediate | Immediate | Needs a deny list |
| Verdict | Rejected: middleware couldn't route keys away from sessions | **Chosen** (the roadmap's format) | Rejected: revocable server-side state is the house rule (ADR-0024) |

Lowercase base32 for both parts: 128-bit lookup (26 characters) and 256-bit secret (52 characters). SHA-256 rather than argon2: the secret has 256 random bits, so a slow hash adds cost without adding security.

### How keys reach the middleware

| | 1. The app's session `Authenticate` checks the prefix | 2. `auth.WithAPIKeys(APIKeyAuthenticator)` option: the middleware routes `gbk_` bearer tokens to it |
|---|---|---|
| "Never a session" | Each app's code must remember | Enforced in the library: session authenticators never see a `gbk_` token, cookies never carry keys, and `NewToken` never returns one |
| Rate-limited answer | Needs a new error path anyway | `*auth.RateLimitError` → 429 `too_many_attempts` |
| Verdict | Rejected | **Chosen**; additive API |

### Where service accounts live, and organisation service accounts

| | 1. Separate module (`modules/apikeys` or app `internal/modules/apikeys`) | 2. The app's auth module owns keys and service accounts; the orgs module reaches them through ports | 3. Organisation service accounts as `org_members` rows |
|---|---|---|---|
| Principal building | Another module in the middleware chain, duplicated role and 2FA logic | Next to sessions and roles in `Authenticate` | Next to sessions |
| Organisations | Import problems either way | `authusecase.OrgAccess` (membership check, role assignment rule, org catalog) implemented by the composition root; `orgs.Service().Memberships()` answers for `svc_` IDs from the auth table | `org_members.user_id` references users; every member query and the last-owner rules would have to exclude them |
| Verdict | Rejected | **Chosen** | Rejected |

`/ops/service-accounts` is registered by the auth module rather than the ops module: the ops module would need a port and copies of the service account and key types to call the same use cases. Its operations are tagged "Ops: service accounts", declare `openapi.Bearer`, and their use cases check `ops.service_accounts.read|write` with `actor.Require`, like `authorize` in the ops module. The two permissions are constants in the auth use cases, declared in `permissions.go` and given to `platform_admin` (both) and `ops_viewer` (read); `opsdomain.AllPermissions` is unchanged.

### Roles that require two-factor authentication

| | 1. Keys get them when the creating session was 2FA-verified | 2. Keys never get them; service accounts can't hold them | 3. A separate "machine 2FA" (mTLS, signed requests) |
|---|---|---|---|
| `/ops` reachable by programs | Yes, with a long-lived bearer secret | No, in v1.1 | Yes |
| Meaning of "requires 2FA" | Weakened: a key is one factor | Kept | Kept, much more work |
| Verdict | Rejected | **Chosen** (the roadmap) | Later, if operators need automation of `/ops` |

### Scopes

| | 1. Scopes replace the owner's permissions | 2. Scopes intersect the owner's current permissions | 3. No scopes |
|---|---|---|---|
| Removing a role from the owner | Keys keep what they had | Applies to the next request | Applies |
| Verdict | Rejected: escalation after demotion | **Chosen** | Rejected: least privilege is the point of keys |

## Decision

Options chosen above. In detail:

### Library (`modules/auth`, `modules/orgs`; additive)

| Addition | Behaviour |
|---|---|
| `NewAPIKey() (key, lookupID, hash)`, `ParseAPIKey`, `IsAPIKey`, `HashAPIKey`, `APIKeyMatches` | Format `gbk_` + 26 + `_` + 52 lowercase base32 characters, exactly 83 bytes; parsing checks only the format; matching uses `subtle.ConstantTimeCompare` on SHA-256 and never branches |
| `APIKeyAuthenticator`, `WithAPIKeys` | A `gbk_` token in `Authorization: Bearer` goes only to it; in a cookie it is ignored; without the option it is ignored. `NewToken` never returns a `gbk_` token |
| `RateLimitError`, `ErrRateLimited` | The middleware answers 429 `too_many_attempts` with `Retry-After` |
| `Principal.APIKeyID`, `Scopes`, `ServiceAccountID`, `OrgID`; `Principal.APIKey()`; `Principal.Restrict(granted, stepUp)` | Restrict leaves sessions unchanged; for a key it drops step-up permissions and keeps only granted permissions in the scopes. `WithPrincipal` applies it and sets a service actor for a service account |
| `DefaultAPIKeyMaxTTL` (90 days), `APIKeyTTLLimits` (1 hour – 1 year), `APIKeyTouchInterval` (1 minute), `APIKeyRetention` (30 days) | Hard limits, as for sessions |
| `orgs.RequireMember`, `orgs.Authorize` | Also accept a service actor whose principal is a service account's key; `Authorize` refuses one outside `Principal.OrgID` (404 `org_not_found`) and passes the role's permissions through `Principal.Restrict` for every key |

### Data (`20260918000020_auth_api_keys.sql`, both apps)

- `auth_service_accounts`: `id` (`svc_`), nullable `org_id` (platform when NULL), `name`, `description`, `roles text[]`, `created_by`, times, `disabled_at`. No foreign key to `orgs`, so the migration is the same in both apps and `orb add orgs` needs nothing; the organisation purge deletes an organisation's service accounts in the same statement.
- `auth_api_keys`: `id` (`key_`), unique `lookup_id`, `secret_hash`, exactly one of `user_id` (cascade) and `service_account_id` (cascade), `name`, `scopes text[]`, required `expires_at`, `created_by`, `created_at`, `last_used_at`, `revoked_at`, `revoked_reason`, `expiry_recorded_at`.

### Authentication (`AuthenticateAPIKey`)

1. Parse; find by lookup ID, joining the user (not deleted) or the service account. An unknown lookup ID compares against an empty hash, like a wrong key.
2. Malformed, unknown or wrong keys charge `auth_api_key` (`auth.api_key_failures_per_minute`, 30 per client network per minute, shared) and answer 401 as anonymous, or 429 past the limit. Revoked, expired and disabled keys answer 401 without charging: they aren't guesses. Only the lookup ID is logged.
3. Permissions: a user's current platform roles, or a platform service account's roles, through `PermissionsFor(roles, false)` and `Restrict`. An organisation service account gets no platform permissions: its role applies through `orgs.RequireMember` in its organisation.
4. `last_used_at` at most once a minute.

### What keys may not do

`requirePrincipal` answers `ErrSessionRequired` (403 `session_required`) for a key, so no key reaches `/v1/auth/me`, sessions, logout, password changes, 2FA, passkeys, identities, account deletion, API keys or service account management. `claimAddress` never treats a key as the account's own signed-in request. A leaked key can't mint keys, change sign-in methods or lock the owner out.

### Endpoints

| Endpoint | Who |
|---|---|
| `GET, POST /v1/auth/api-keys`, `DELETE /v1/auth/api-keys/{id}` | The signed-in user. Creating needs a verified address and `confirmUser` (the password, or a second factor verified within 10 minutes; a 2FA-verified session when 2FA is on) and spends `auth.reauth_attempts`. Scopes must be the user's non-2FA platform permissions or organisation permissions some non-2FA org role grants |
| `GET, POST /ops/service-accounts`, `GET, PATCH, DELETE /ops/service-accounts/{id}`, `GET, POST …/{id}/keys`, `DELETE …/{id}/keys/{keyId}` | `ops.service_accounts.read`/`.write` with a 2FA session. Roles: declared, not `RequireMFA`, at most 10. Key scopes ⊆ the roles' permissions; creating a key also runs `confirmUser` |
| The same under `/v1/orgs/{orgId}/service-accounts` (multi-tenant apps) | Members whose role grants `orgs.service_accounts.manage` (owner, admin). One org role that the member could give a member (`canAssign`), never `owner` or a `RequireMFA` role; a member can't manage a service account whose role they couldn't give. Reads through `orgs.RequireMember` on members only, so service accounts never manage organisations |

Every creation response carries `Cache-Control: no-store`. At most 20 usable keys per owner and 100 service accounts per organisation or platform.

### Revocation

Revoking a key, disabling a service account (revokes every key; enabling doesn't restore them), deleting a service account (deletes keys), deleting an account, resetting the password and claiming an address (revoke the account's keys, in the same transaction as ending sessions). A deleted account's key fails even before revocation, since the account is joined on every request. Deleting an organisation stops its service accounts' keys (the membership read requires a live organisation); purging deletes them. Changing the password while signed in keeps keys, as integrations shouldn't break when a person rotates a password; resetting it, the recovery path, doesn't.

### Settings, audit, jobs

- Settings: `auth.api_key_max_ttl` (90 days, 1–365 days), `auth.api_key_failures_per_minute` (30, 5–10 000, group `rate_limits`); both require a reason.
- Audit: `auth.api_key.created`, `auth.api_key.revoked` (per key with `reason`, or per owner with `count`), `auth.api_key.expired`, `auth.service_account.created`, `.updated`, `.disabled`, `.deleted`. Metadata carries owner, lookup ID, scopes and expiry, never the key or its hash.
- No new job: `auth_cleanup` records each expiry once (`expiry_recorded_at`, `SKIP LOCKED`, 500 per statement) and deletes keys expired or revoked more than 30 days ago.

## Why

- A prefix-routed key in the library makes "never a session" and "never logged" properties of the framework rather than of each app's code.
- Reading roles on every request and intersecting with scopes means demotions, disabling and revocation apply to the next request, and a key can never hold more than its owner does.
- Refusing 2FA roles to keys keeps the meaning of `RequireMFA`: a bearer secret is one factor.
- Session-only account management turns a leaked key into a bounded incident: it can be revoked, and it can't make itself permanent.
- Putting service accounts beside users in the auth module keeps one place where principals and permissions are computed, while organisations still decide membership through their own module.

## Trade-offs

- `/ops` can't be automated with keys in v1.1; operators script with sessions until a stronger machine credential exists.
- ~~Operations that need only a signed-in user, with no permission, aren't limited by scopes.~~ Closed by "Scopes cover every operation (2026-09-16)" below.
- Personal keys act in every organisation the user belongs to; per-organisation personal keys are a later option.
- Keys survive a password change made while signed in.
- The failure limit counts per client network, so many clients behind one NAT share it; valid keys aren't affected.
- Platform service accounts in the example apps have no roles they can hold until the app declares a role without `RequireMFA`.
- One more lookup per key request, plus a roles query for user keys, like sessions.
- `/ops/service-accounts` lives in the auth module, not the ops module, in exchange for no duplicated types.

## Consequences

- `modules/auth` and `modules/orgs` gain additive API (`api/modules-auth.txt`); both Full apps gain the migration, repository files, use cases, endpoints, settings, limiter, permissions, error codes and audit actions (`api/surface.json`); full-multi gains `orgs.service_accounts.manage`, `internal/app/orgs_service_accounts.go` and org routes.
- ADR-0024: API keys are designed here. ADR-0038: `requirePrincipal` means a signed-in session; password reset and address claims revoke keys. ADR-0043: roles that require 2FA never grant keys. ADR-0048: `orgs.RequireMember` accepts organisation service accounts; the purge deletes them.
- Threat model: new rows for API key leakage and scope escalation (notes for the lead).
- Guide: [API keys and service accounts](../guides/api-keys.md).

## Implementation notes (2026-09-16)

- `auth.Principal.Restrict` is applied twice on purpose: by the app when building the principal, and by `auth.WithPrincipal` and `orgs.Authorize`, so an app's mistake can't hand a key step-up permissions.
- `SelectAPIKeyByLookupID` joins the user with `deleted_at IS NULL` and the service account, so deleting an account stops its keys in the same request, before revocation.
- Revoking reports whether this call revoked the key through a `FOR UPDATE` CTE, so a repeated revoke records one event.
- `orgs.Service().Memberships()` now returns a wrapper: `svc_` IDs are answered by `ServiceAccountRole` (enabled, exactly one role, live organisation), others by `MemberRole`. The orgs module's own use cases keep calling `RequireMember` with the members-only store.
- The failure limiter can't peek, so a request is evaluated before it is charged; with 256-bit secrets, evaluating guesses leaks nothing.
- `internal/app/app_test.go` gains `signedInEndpoint` (`/v1/projects` in full-single, `/v1/orgs` in full-multi) so `apikeys_test.go` is identical in both apps.

| Check | Result |
|---|---|
| `modules/auth` `TestNewAPIKey`, `TestParseAPIKey` (22 malformed shapes: prefix case, lengths, separators, padding, non-base32, Cyrillic lookalike, NUL, doubled prefix), `TestAPIKeyMatches` (first and last byte, nil, short and long hashes) | pass |
| `TestAPIKeyMatchesInConstantTime` | The comparison uses `subtle.ConstantTimeCompare` and no early return, branch, loop or `bytes.Equal` |
| `TestMiddlewareAPIKeys`, `TestNewTokenIsNeverAnAPIKey`, `TestPrincipalRestrict` | Keys never reach the session authenticator, not from cookies, not without `WithAPIKeys`; actor limited to scopes without step-up even when the authenticator returns more; service actor; 429 with `Retry-After`; 503 on store errors |
| `modules/orgs` `TestRequireMemberWithAPIKeys` | Scopes limit org permissions; 2FA roles unreachable; a service account's key refused in another organisation even when the store claims a role there |
| Use cases (Docker PostgreSQL): `TestAPIKeyAuthentication`, `TestDeletedOrDisabledOwnersKeysFail`, `TestAPIKeysNeverCarryTwoFactorPermissions`, `TestAPIKeyScopesCantEscalate`, `TestAPIKeysNeedASession`, `TestCreateAPIKeyChecksTheUserAndInput`, `TestMaxTTLSetting`, `TestServiceAccountsNeedOpsPermissions`, `TestOrganisationServiceAccounts`, `TestAPIKeyFailuresAreLimited`, `TestCleanupRecordsExpiredAPIKeys`, `TestAPIKeysNeverLogged` | pass; logs at debug level and every audit event hold lookup IDs but never a secret or hash |
| Both Full apps: `TestAPIKeysEndToEnd`, `TestServiceAccountsThroughOps` | Password required, `no-store`, key as bearer, `session_required` on 8 management endpoints, cookie and wrong keys 401, revoke, an operator's key 403 on `/ops`, 429 after 30 wrong keys, nothing stored in audit or jobs |
| full-multi `TestOrganisationsCantReachEachOthersServiceAccounts` | Another organisation's owner gets 404 on 11 routes; a service account's key works in its organisation, 404 elsewhere and on organisation management; read-only personal key 403 on writes; disabled 401; deleted organisation 404; purge deletes service accounts and keys |
| Mutations (each reverted): no `Restrict` in `orgs.Authorize` or `WithPrincipal`; no organisation check for service actors; keys from cookies to sessions; `requirePrincipal` accepting keys; `PermissionsFor(roles, true)` for keys; no scope check; no expiry check; 2FA org roles allowed; no failure limit | Each makes the corresponding test above fail |
| `go run -C internal/tools/apicheck .` | additions only, recorded |
| `TestPublicSurface`, `TestOpsAPICompatible`, `TestOpenAPIUpToDate`, `TestGoldenAppsDontDrift` | pass after recording; `/ops` changes are additions |
| golangci-lint | not installed locally; not run |

## Scopes cover every operation (2026-09-16)

### Context

Scopes were intersected with the owner's permissions, so they limited only operations that check a permission. A security review of the merged feature found operations that check none:

| Operation | Check before | A key scoped to `projects.project.read` could |
|---|---|---|
| Single-tenant projects, and every `orb gen resource --scope user` resource | Ownership only (`ownerID`) | Create, change and delete the user's projects |
| `POST /v1/orgs`, `GET /v1/orgs` (multi-tenant) | A signed-in user (`userID`) | Create organisations, list them |
| `POST /v1/invitations/accept` | A signed-in user; the verified address must match | Join organisations, widening what every key of the account reaches |
| `POST /v1/orgs/{orgId}/leave`, removing yourself | `orgs.org.read` through `RequireMember` | Take the person out of an organisation (needs an invitation to undo) |
| Account, session, key and service account management | `requirePrincipal` answers `session_required` | Nothing (already closed) |
| Other organisation operations (rename, delete, restore, members, invitations, settings, projects) | An org permission through `orgs.RequireMember`/`Authorize`, which applies `Restrict` | Nothing beyond its scopes (already closed) |

### Options

| | 1. A user role every user holds, with a permission for each signed-in operation | 2. Deny by default: a scoped key may only call operations that check one of its scopes | 3. Document it (the original trade-off) |
|---|---|---|---|
| Mechanism | `authusecase.RoleUser` (`user`) declared in `permissions.go`; `Authenticate` and `AuthenticateAPIKey` add it to a user's roles; use cases call `actor.Require` | The request would have to know, before the handler runs, which permission the operation will check, or record the checks made and refuse afterwards |
| Enforceable for every operation | By convention and tests: a new signed-in operation without a check is a bug the guide and template prevent, not something the framework can detect | Not mechanically: a check is made inside the use case, after routing; refusing after the handler ran is too late for writes, and a per-operation declaration (OpenAPI extension) could drift from the use case. Public endpoints (ping, sign-in) check nothing and must keep working for everyone | — |
| Sessions | Unchanged: the role always grants its permissions | Unchanged | — |
| Empty scopes | Everything the owner holds without 2FA, the user role included | Would need a special case | — |
| Verdict | **Chosen** | Rejected: can't be enforced mechanically | Rejected: a read-only key must be read-only |

For joining and leaving organisations a scope isn't enough: accepting an invitation widens what every key of the account reaches (personal keys act in all the user's organisations), and leaving can't be undone without a new invitation. They follow account management: sessions only.

### Decision

| Operation | A key may | Check |
|---|---|---|
| User-scoped resources (example projects in full-single, generated `--scope user`) | Within scopes | `ownerID(ctx, PermRead/PermWrite)` → `actor.Require`; `ErrForbidden` → 403 `forbidden`. Permissions `<module>.<resource>.read`/`.write` listed in `userResourcePermissions` (anchor `//orb:anchor user-permissions`), granted by `user` |
| Create and list organisations | Within scopes | `orgs.org.create`, `orgs.org.list` (platform permissions of `user`); `orgsdomain.ErrForbidden` → 403 `forbidden` |
| Accept an invitation, leave an organisation, remove yourself | Never | `requireSession`: a principal with `APIKey()` → `orgsdomain.ErrSessionRequired` → 403 `session_required` |
| Rename, delete, restore an organisation; manage other members and invitations; organisation settings and resources | Within scopes and the owner's org role | Unchanged: `orgs.RequireMember`/`Authorize` with `Restrict`. Deleting is a soft delete an owner can undo, and an unscoped key can already delete every resource, so a separate session rule would add little |
| Account, sessions, 2FA, passkeys, identities, API keys, service accounts (platform and organisation) | Never | Unchanged: `requirePrincipal` → `session_required` |
| `/ops` | Never | Unchanged: ops roles require 2FA |

The `user` role: declared by the app (both Full apps), required by `authusecase.NewService` (declared, never `RequireMFA`), added to a user's roles for sessions, user keys and scope validation (`userPermissions`), never to service accounts (refused as a service account role), and not grantable (`GrantRole` answers `ErrUnknownRole`; `grant-role` says every account holds it). Its permissions are ordinary scopes: `POST /v1/auth/api-keys` accepts them.

### Trade-offs

- The property holds by convention: an operation added by hand without a permission check isn't limited by scopes. The guide says every signed-in operation must check one, and the resource template does.
- Apps must declare the `user` role; an app without it fails at startup with a clear error (upgrade notes).
- Sessions' permission lists (and `/v1/auth/me`) now include the user role's permissions.
- Personal keys can't accept invitations or leave organisations even when unscoped; automation that did that needs a session.
- A resource can be user-scoped in one app and org-scoped in another under the same permission name (`projects.project.read`), in different catalogs; the reference marks which apps declare each.

### Implementation notes

| Check | Result |
|---|---|
| Use cases (both apps): `TestUserRoleIsScopedLikeAnyRole` | Session without granted roles holds the user role's permissions; read-scoped key refused writes; unscoped key allowed; `GrantRole(user)` and a service account with `user` refused; `NewService` refuses a catalog without the role or with it requiring 2FA |
| `TestAPIKeyScopesCantEscalate`, `TestAPIKeysNeverCarryTwoFactorPermissions` and session tests updated for the user role | pass |
| projects use cases: `TestRequiresPermission` (generated for every user-scoped resource) | pass |
| full-multi orgs use cases: `TestAPIKeysNeedScopesOrASession` | Create/List `ErrForbidden` without the scopes, allowed with them; rename refused outside scopes; Leave, RemoveMember(self), AcceptInvitation `ErrSessionRequired` for an unscoped key; the session then accepts |
| Both apps: `TestAPIKeyScopesCoverOwnData` (shared `apikeys_test.go`) | Read-only key: GET list and item 200; POST, PATCH, DELETE 403 `forbidden`; write-only key can't list; session updates and an unscoped key deletes; undeclared scope 422 |
| Both apps: `TestProjectsEndToEnd` (generated) | Read-scoped key 200 on reads, 403 `forbidden` on writes (user-scoped and org-scoped) |
| full-multi `TestAPIKeysAndOrganisations` | Projects-only key 403 on `GET`/`POST /v1/orgs`; `orgs.org.create`/`list`/`read` key creates, lists, reads, 403 on rename, members, invitations and projects; unscoped key 403 `session_required` on accept, leave and removing itself, still reaches the organisation; the session accepts and leaves |
| Mutations: no `actor.Require` in the projects `ownerID`; no permission check in orgs `requireUser`; no key check in `requireSession` | Each makes the tests above fail |
| `TestResourceMatchesGoldenApp`, `TestGeneratedResourcesPass` (a user-scoped Note generated into both golden apps), `TestGenResource*` | pass |
| `TestPublicSurface` (recorded: role `user`; full-single `projects.project.read|write`, full-multi `orgs.org.create|list`), `TestOpsAPICompatible`, `TestOpenAPIUpToDate`, `TestGoldenAppsDontDrift` | pass |

