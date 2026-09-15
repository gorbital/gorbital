# ADR-0038: Authentication in v0.2

**Status:** Accepted (2026-09-15) · **Amends:** ADR-0024, ADR-0026, ADR-0029 · **Supersedes:** ADR-0034 · **Amended by:** ADR-0048 (multi-tenant apps create a personal workspace with each account and check ownership before deleting one)

## Context

ADR-0024 chose embedded authentication with server-side sessions, argon2id passwords, hashed email codes and platform roles. Building `modules/auth` and wiring it into `examples/full-single` settled the details it left open: the data model, how browsers and native clients receive tokens, how limits and expiries are enforced when some are runtime settings (ADR-0031), how the first administrator is created, and how `/ops/*` moves from the interim `OPS_TOKEN` (ADR-0034) to real users and roles.

## Decision

### Who owns what

A generated app owns its authentication like any other module, with all four layers, the way a hand-built API keeps `internal/modules/auth`. The library holds only building blocks, the role an app's own `internal/platform` would play.

| | Library: `apistock.dev/modules/auth` (updated with `go get`) | App: `internal/modules/auth` (generated, owned, editable) |
|---|---|---|
| Passwords | `Hasher` (argon2id, rehash detection, `VerifyDummy`), `ValidatePassword`, `PasswordChecker` | Calls them in register, login, reset and change |
| Tokens and codes | `NewToken`, `HashToken`, `NewCode`, `HashCode`, `CodeMatches`, `NewID`, `NormalizeEmail`, `ClientInfo` | Stores only hashes |
| HTTP sessions | `Middleware(authenticator)`, `SessionCookie`, `ClearSessionCookie`, `TokenFrom`, `Principal`, `PrincipalFrom` | `usecase.Service.Authenticate` implements `auth.Authenticator` |
| Roles | `Catalog`: permissions and roles, deny by default | `internal/app/permissions.go` declares them; `auth_user_roles` stores assignments |
| Limits | Defaults and hard limits (`Limits.Clamp`) | Clamps runtime settings before using them |
| Emails | `Emails` port and `NewMailEmails` plain templates | Passes the queued mailer |
| Flows | none | `usecase/`: register, verify, resend, login, authenticate, me, sessions, logout, reset, change password, delete account, create user, roles, cleanup |
| Storage | none | `repository/`: `store.go` with `InTx`, one SQL file per operation (ADR-0032); tables in `db/migrations` |
| HTTP | none | `delivery/`: the `/v1/auth` operations |
| Tests | Helper tests without a database | Use cases on the real repository and Docker PostgreSQL, repository tests, an end-to-end HTTP test |

This amends ADR-0024 ("logic lives in `apistock.dev/modules/auth`"): the logic lives in the app, where developers read and change it. A fix to a flow reaches existing apps through `aps upgrade` (ADR-0016); fixes to hashing, tokens, codes and cookies still arrive with `go get`.

Tables: `auth_users`, `auth_sessions`, `auth_codes`, `auth_user_roles`; text IDs with prefixes (`usr_`, `ses_`, `cod_`) and 128 random bits; nullable `org_id` on sessions and role assignments for organisations (ADR-0023).

### Passwords and codes

| Topic | Decision |
|---|---|
| Hashing | argon2id with m=19 MiB, t=2, p=1, 16-byte salt, 32-byte key, stored as a PHC string; hashes with older parameters are replaced at the next login; concurrent hashing bounded by a semaphore |
| Policy | 12–128 characters, not blank; an optional `PasswordChecker` (for example a breached-password list) returns a reason shown to the user |
| Codes | 6 random digits; stored as SHA-256 of the code ID and code; 5 attempts; the newest code of a purpose replaces older ones; a new code at most once a minute per account |
| Expiry | Verification 15 min, reset 30 min by default |

### Sessions

| Topic | Decision |
|---|---|
| Token | 32 random bytes, base64url; only its SHA-256 is stored |
| Transport | Login takes `"transport": "cookie"` (default, browsers: `__Host-session`, Secure, HttpOnly, SameSite=Lax, lasting to the absolute expiry) or `"bearer"` (native clients: the token in the response, sent as `Authorization: Bearer`) |
| Middleware | `auth.Middleware(authenticator)` stores the client IP and user agent for every request and, for a valid bearer or cookie token, the principal and its actor. Invalid tokens continue anonymously; use cases decide what needs authentication. A store failure returns 503 rather than silently treating a user as anonymous |
| Expiry | Idle 14 days and absolute 90 days by default; each use more than a minute after the last extends the idle expiry, up to the absolute one |
| Permissions | Read from the user's roles on every request, so a grant or revocation applies immediately; no token rotation is needed for privilege changes, and tokens are only created at login, so session fixation isn't possible |
| CSRF | Cookie-authenticated requests rely on `httpx.CrossOrigin` (Fetch metadata and Origin checks) earlier in the chain |
| Ending sessions | Logout, logout all, revoke one; password change ends other sessions; password reset and account deletion end all |

### Hard limits and runtime settings

Durations come from runtime settings but the library clamps them, so an operator (or a compromised ops account) can't weaken security past these limits (threat 23):

| Setting (example app) | Default | Settings range | Library limit |
|---|---|---|---|
| `auth.session_idle_ttl` (reason required) | 14 days | 1 hour – 90 days | 5 minutes – 90 days |
| `auth.session_absolute_ttl` (reason required) | 90 days | 1 day – 365 days | 1 hour – 365 days |
| `auth.verification_code_ttl` | 15 minutes | 5 minutes – 1 hour | 1 minute – 1 hour |
| `auth.reset_code_ttl` (reason required) | 30 minutes | 10 minutes – 2 hours | 1 minute – 2 hours |
| `auth.deleted_account_retention` (reason required) | 30 days | 1 day – 365 days | 0 – 365 days |

### Account enumeration and brute force (threat 12)

- Registration returns the same 202 whether or not the address has an account; an existing verified account's owner gets an "account exists" email instead of a code, at most once a minute per address per instance.
- Registering again before verifying keeps the account's password and sends a new code, at most once a minute. *Amended 2026-09-15 after review:* replacing the password let anyone who registered an address last choose the password of the account its owner then verified, and sending a code on every registration bypassed the resend limit, allowing unlimited fresh codes to guess. An owner who forgot the password before verifying uses password reset, which also verifies the address.
- Login returns `invalid_credentials` for unknown addresses and wrong passwords after the same hashing work; `email_not_verified` only after a correct password.
- Resend and forgot-password always return 202.
- Limits: 10 login attempts per address per 15 minutes per instance (`too_many_attempts`); 5 attempts per code; a new verification or reset code at most once a minute per account, whether requested by resend, forgot-password or registering again; 60 changing requests per minute per client IP on `/v1/auth/*` in the app.

### Accounts

- Password reset verifies the address, sets the password and ends every session; a "password changed" email follows.
- Account deletion requires the password, soft-deletes (the address can register again at once) and ends every session; `Cleanup` removes deleted accounts after the retention, ended sessions after 7 days and codes after a day. The example runs it as the `auth_cleanup` job daily at 03:30 UTC.
- `CreateUser` lets an operator create a (verified) account; `UserByEmail` is for operators only.

### Permission catalog and platform roles

| Topic | Decision |
|---|---|
| Catalog | Declared in code before `auth.New`: permissions (`module.resource.action`) and roles as sets of declared permissions; invalid or late declarations panic at startup |
| Deny by default | A user has only the permissions of their roles; stored role names missing from the catalog grant nothing |
| Example roles | `platform_admin` (every `ops.*` permission) and `ops_viewer` (`ops.settings.read`, `ops.jobs.read`, `ops.audit.read`, `ops.mail.read`), in `internal/app/permissions.go` |
| Granting | `GrantRole` / `RevokeRole` with an actor; the example's `go run ./cmd/api grant-role <email> <role>` (and `revoke-role`, `roles`) runs as the `cli` system actor and is audited |

### Ops APIs

`OPS_TOKEN` and `ops_auth.go` are removed. `/ops/*` requests authenticate with a session; the ops use cases' existing permission checks return 401 `unauthenticated` without one and 403 `forbidden` without the permission. Changes are attributed to the signed-in user in history, job metadata and audit events. Required 2FA for ops roles remains v0.3 (ADR-0024).

### Audit actions

`auth.user.registered`, `auth.user.created`, `auth.email.verified`, `auth.login.succeeded`, `auth.login.failed` (metadata `reason`: `invalid_credentials`, `email_not_verified`, `rate_limited`), `auth.session.revoked`, `auth.sessions.revoked`, `auth.password.reset_requested`, `auth.password.reset`, `auth.password.changed`, `auth.account.deleted`, `auth.role.granted`, `auth.role.revoked`, `auth.accounts.purged`. Events carry the client IP and user agent; email addresses are never in metadata.

### Example endpoints

| Endpoint | Result |
|---|---|
| `POST /v1/auth/register` | 202 |
| `POST /v1/auth/verify-email`, `POST /v1/auth/verify-email/resend` | 204, 202 |
| `POST /v1/auth/login` | 200 with user and session; cookie or token |
| `POST /v1/auth/password/forgot`, `POST /v1/auth/password/reset` | 202, 204 |
| `GET /v1/auth/me`, `DELETE /v1/auth/me` | 200 with permissions; 204 |
| `POST /v1/auth/logout`, `POST /v1/auth/logout-all` | 204; 200 with the count |
| `PUT /v1/auth/password` | 204 |
| `GET /v1/auth/sessions`, `DELETE /v1/auth/sessions/{id}` | 200; 204 |

Error codes: `unauthenticated`, `invalid_email`, `weak_password` (detail says why), `invalid_credentials`, `email_not_verified`, `invalid_code`, `too_many_attempts` (detail says how long), `session_not_found`, `auth_unavailable` (503).

Core `httpx.Mapper` now lets several errors share a code when their status matches, so `ops` and `auth` both map to `unauthenticated`.

### Not in v0.2

2FA and passkeys (v0.3), social sign-in (v0.3), new-device alert emails, changing the email address, owned email templates with a preview route, trusted-proxy client IP handling, a shared rate-limit store across instances.

## Why

- Sessions stored as hashes are revocable at once and leak nothing useful from a database dump.
- Developers see and own every step of sign-up and sign-in, with the same layers as their other modules, while the cryptographic building blocks stay in the library, so fixes to them arrive with `go get`.
- Clamping settings inside the library keeps the admin dashboard useful without letting it become a way to weaken security.
- Roles read on every request make permission changes immediate and simple to reason about.

## Trade-offs

- Rate limits are per instance; several instances multiply them until a shared store exists.
- The per-address login limit lets someone slow down another person's logins for 15 minutes; there is no hard lockout.
- Reading roles on every request costs one query per authenticated request.
- Soft deletion keeps personal data until the retention ends; it is documented and configurable.

## Consequences

- Threat model rows 12 and 13 are addressed; row 18 is addressed except required 2FA (v0.3).
- Permission names, role names, setting keys, error codes, audit actions and `auth_users.id` are public API (ADR-0015).
- Guide: [authentication](../guides/authentication.md).
