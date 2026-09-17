# ADR-0070: Operators' account APIs

**Status:** Accepted (2026-09-16) · **Amends:** ADR-0038, ADR-0043, ADR-0044, ADR-0052, ADR-0058, ADR-0066

## Context

Phase 5 of the [Dev Portal roadmap](../dev-portal-roadmap.md) is the Authentication screen: see and search accounts, create one, verify an address, end sessions, remove passkeys and provider links, reset second factors, ban, delete, act as a user to test the API, and unblock a rate-limited client. Today the auth module serves people's own accounts (`/v1/auth/*`) and a handful of operator commands (`admin.go`: grant and revoke roles, reset MFA; seed data), and `/ops/auth/providers` reports the sign-in methods. There is no operator API for accounts, no ban, and rate-limit budgets can be neither seen nor forgotten (keys are stored hashed).

## Options

### Where the endpoints live

| Option | Verdict |
|---|---|
| The ops module, calling the auth module | Rejected: modules never import each other (`architecture_test.go`); the ops module's `/ops/auth/providers` reads configuration, not accounts |
| **The auth module, under `/ops/auth/users…`, as it already serves `/ops/service-accounts`** | **Chosen**: the use cases sit next to the account rules they must respect (hooks, revocations, audit) |

### Who may call them

| Option | Verdict |
|---|---|
| `ops.auth.read` for everything | Rejected: `ops_viewer` reads without changing anything |
| **`ops.auth.read` for reads (held by `ops_viewer` and `platform_admin`); a new `ops.auth.write`, declared by the auth module and granted to `platform_admin`, for writes; the development operator (ADR-0066) holds both. The use cases require an actor, never a session, so the dev console's system actor works** | **Chosen**: the existing split, applied to accounts |

### Bans

| Option | Verdict |
|---|---|
| Soft-delete as a ban | Rejected: deletion frees the address and purges data after the retention period; a ban must be reversible and keep the account |
| **`auth_users.banned_at` and `banned_reason`; banning revokes every session and API key; every path that starts a session (password, second factor, passkey, Google, Apple, GitHub, impersonation) refuses a banned account with `ErrAccountBanned` (403 `account_banned`); `Authenticate` treats a banned account's session as unauthenticated** | **Chosen**: one check in `startSession` covers every sign-in; the ban is visible to operators and reversible |

### Impersonation

| Option | Verdict |
|---|---|
| Always available to `platform_admin` | Rejected: a production administrator acting as a user is a different decision, with consent and audit questions this record doesn't settle |
| **`Impersonate` exists only while the app runs with the dev console (`Config.Impersonation` set from `devConsoleOn()`); elsewhere it answers 403 `impersonation_off`; the session is audited as `auth.user.impersonated` with the operator as actor; `mfa_verified` decides whether roles requiring a second factor apply** | **Chosen**: the Dev Portal's route tester and auth playground need it; production never has it |

### Rate limits

Budgets can't be listed (keys are hashed). `ratelimitpg.(*Store).Reset(name, key)` forgets one bucket; the app lists its limiters with what their keys are (`rateLimits.Limiters`), and `POST /ops/auth/rate-limits/reset` forgets a key's budget, audited as `ops.rate_limit.reset` naming the limiter only (keys may be addresses).

### Codes

Verification and reset codes are stored hashed, so the API reports that a usable code exists (purpose, attempts, expiry), never the code: the Dev Portal reads it from the local inbox (`/_dev/mail`), as a person would.

## Decision

| Piece | Decision |
|---|---|
| Endpoints (`Ops: auth`) | `GET /ops/auth/users` (`q`, `cursor`, `limit`; newest first; banned listed, deleted not), `POST /ops/auth/users` (email, password, `email_verified`), `GET /ops/auth/users/{id}` (user, sessions, passkeys, identities, MFA status, codes), `DELETE /ops/auth/users/{id}`, `POST …/verify-email`, `POST …/ban` (reason), `POST …/unban`, `POST …/roles`, `DELETE …/roles/{role}`, `DELETE …/sessions`, `DELETE …/sessions/{sessionId}`, `DELETE …/passkeys/{passkeyId}`, `DELETE …/identities/{identityId}`, `POST …/mfa/enroll` (secret and recovery codes, once), `POST …/mfa/reset`, `POST …/impersonate` (`mfa_verified`); `GET /ops/auth/rate-limits`, `POST /ops/auth/rate-limits/reset` (name, key) |
| Use cases (`usecase/operators.go`) | `ListUsers` (keyset cursor on created_at and id), `UserDetail`, `VerifyUserEmail`, `BanUser`, `UnbanUser`, `DeleteUser` (the owner's deletion without the password: hooks, identities unlinked with token revocations queued, keys and sessions revoked), `RevokeUserSession`, `RevokeUserSessions`, `RemoveUserPasskey`, `RemoveUserIdentity`, `Impersonate`; `GrantRole`, `RevokeRole`, `EnrollTOTP` and `ResetMFA` as before behind `AuthorizeOps` |
| Audit actions | `auth.email.verified_by_operator`, `auth.user.banned` (reason), `auth.user.unbanned`, `auth.user.deleted_by_operator`, `auth.session.revoked_by_operator`, `auth.passkey.removed_by_operator`, `auth.identity.removed_by_operator`, `auth.user.impersonated` (session, `mfa_verified`), `ops.rate_limit.reset` |
| Error codes | `account_banned` (403), `impersonation_off` (403), `user_not_found` (404), `rate_limiter_not_found` (404); `invalid_cursor` (400) as elsewhere |
| Migration | `20260918000070_auth_bans.sql`: `banned_at timestamptz`, `banned_reason text NOT NULL DEFAULT ''` on `auth_users` |
| Library | `ratelimitpg.(*Store).Reset(ctx, name, key) (bool, error)` |
| The portal | The Authentication screen: users with search and paging, detail with actions, sessions, passkeys, identities, MFA, codes with a link to the inbox, providers (from `/ops/auth/providers`) and rate limiters with reset; "Act as user" fills the route tester's bearer token |

## Why

- Putting the use cases in the auth module keeps every account rule in one place: a ban revokes what a deletion revokes; a deletion by an operator runs the organisation hooks.
- A separate write permission keeps `ops_viewer` read-only without a new role.
- Impersonation tied to the dev console is the same boundary the dev operator uses: on in development, impossible in production.

## Trade-offs

- Codes stay invisible to the API; the inbox is the way.
- A ban revokes API keys, which stay revoked after an unban; the person creates new ones.
- Listing users by a substring of the address is a sequential scan; fine for development and small platforms, indexed later if needed.

## Consequences

- Threat model ([ADR-0029](0029-threat-model.md)) row 18 gains the operators' account APIs (write permission, audit, impersonation only in development); row 3 (session theft) notes impersonation sessions are audited.
- Upgrade notes: Full apps get the endpoints, the migration, `ops.auth.write` in `platform_admin`, `Impersonation` in the auth config, and the ops module's rate limit admin through `orb upgrade`; `api/surface.json` is re-recorded.
- Guides: [ops API](../guides/ops-api.md), [authentication](../guides/authentication.md), [Dev Portal](../guides/dev-portal.md).

## Implementation notes (2026-09-16)

`TestOpsUsers` (both Full apps): create, list with paging, detail, revoke sessions, roles, ban (sign-in 403 `account_banned`, sessions gone), unban, impersonate (`/v1/auth/me` as the user), MFA enroll and reset, the audit log, delete, and the `ops_viewer` refusal; `TestOpsImpersonationOffWithoutTheConsole`; `TestOpsRateLimits`; `TestReset` in `modules/ratelimitpg`.
