# ADR-0038: Authentication in v0.2

**Status:** Accepted (2026-09-15) · **Amends:** ADR-0024, ADR-0026, ADR-0029 · **Supersedes:** ADR-0034 · **Amended by:** ADR-0048 (multi-tenant apps create a personal workspace with each account and check ownership before deleting one), ADR-0053 (pre-registration takeover, code budget, response-time floor, per-network sign-in limit, bounded hashing, unverified account expiry), ADR-0058 (API keys: account management needs a signed-in session; password reset and address claims revoke keys)

## Context

ADR-0024 chose embedded authentication with server-side sessions, argon2id passwords, hashed email codes and platform roles. Building `modules/auth` and wiring it into `examples/full-single` settled the details it left open: the data model, how browsers and native clients receive tokens, how limits and expiries are enforced when some are runtime settings (ADR-0031), how the first administrator is created, and how `/ops/*` moves from the interim `OPS_TOKEN` (ADR-0034) to real users and roles.

## Decision

### Who owns what

A generated app owns its authentication like any other module, with all four layers, the way a hand-built API keeps `internal/modules/auth`. The library holds only building blocks, the role an app's own `internal/platform` would play.

| | Library: `gorbital.dev/modules/auth` (updated with `go get`) | App: `internal/modules/auth` (generated, owned, editable) |
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

This amends ADR-0024 ("logic lives in `gorbital.dev/modules/auth`"): the logic lives in the app, where developers read and change it. A fix to a flow reaches existing apps through `orb upgrade` (ADR-0016); fixes to hashing, tokens, codes and cookies still arrive with `go get`.

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
- Registering again before verifying keeps the account's password and sends a new code, at most once a minute. *Amended 2026-09-15 after review:* replacing the password let anyone who registered an address last choose the password of the account its owner then verified, and sending a code on every registration bypassed the resend limit, allowing unlimited fresh codes to guess. An owner who forgot the password before verifying uses password reset, which also verifies the address. *Amended 2026-09-16 after security review (AUTH-S-1):* keeping the first registrant's password let whoever registered first choose the password of the account its owner then verified; registering again now keeps the password only when it is the same one, and verifying removes whatever the account got before (see "Security review fixes").
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

2FA and passkeys (v0.3), social sign-in (v0.3), new-device alert emails, changing the email address, owned email templates with a preview route, trusted-proxy client IP handling and a shared rate-limit store across instances (both done in [ADR-0052](0052-shared-rate-limits.md)).

## Why

- Sessions stored as hashes are revocable at once and leak nothing useful from a database dump.
- Developers see and own every step of sign-up and sign-in, with the same layers as their other modules, while the cryptographic building blocks stay in the library, so fixes to them arrive with `go get`.
- Clamping settings inside the library keeps the admin dashboard useful without letting it become a way to weaken security.
- Roles read on every request make permission changes immediate and simple to reason about.

## Trade-offs

- Rate limits are per instance; several instances multiply them until a shared store exists. Resolved by [ADR-0052](0052-shared-rate-limits.md): limits are shared in PostgreSQL.
- The per-address login limit lets someone slow down another person's logins for 15 minutes; there is no hard lockout.
- Reading roles on every request costs one query per authenticated request.
- Soft deletion keeps personal data until the retention ends; it is documented and configurable.

## Consequences

- Threat model rows 12 and 13 are addressed; row 18 is addressed except required 2FA (v0.3).
- Permission names, role names, setting keys, error codes, audit actions and `auth_users.id` are public API (ADR-0015).
- Guide: [authentication](../guides/authentication.md).

## Security review fixes (2026-09-16)

The internal security review of September 2026 (findings AUTH-S-1 to AUTH-S-8) changed these parts of the decision. Library changes arrive with `go get`; app changes with `orb upgrade`.

| Finding | Before | Now | Why this design |
|---|---|---|---|
| AUTH-S-1 pre-registration takeover (High) | Registering again kept the first registrant's password; verifying only marked the address verified | Registering again for an unverified account keeps its password only when the same password is given (checked with argon2), and otherwise removes it (`RemovePassword`), throttled or not. Verifying by code, by password reset or by a Google or Apple link runs `claimAddress` in the same transaction: every session ends and passkeys, the authenticator app, recovery codes and identities (Apple tokens queued for revocation) are removed | The triage proposed storing each registration's password with its code and setting it on verification. That lets whoever registers last win: an attacker re-registering every minute replaces the owner's code with one carrying the attacker's password, and the owner enters the newest code in their mailbox. With two different passwords nobody has proven ownership, so no password survives; the owner sets one with password reset, which proves the mailbox. A single registrant, and an owner submitting the form twice, keep theirs |
| AUTH-S-2 code guessing across codes (Medium) | 5 attempts per code, a new code every minute | Also `auth.code_attempts` (20) checks per `auth.code_window` (24 h) per purpose and normalized address, in the shared `auth_code` limiter, charged before the lookup whether or not the address has an account (429 `too_many_attempts`) | Keyed by address, not account, so the limit answers the same for unknown addresses. Past the budget the owner's right code is refused too until the window passes; password sign-in is unaffected |
| AUTH-S-3 Unicode case folding (Low) | `strings.ToLower` mapped the Kelvin sign to `k`, so `Kevin@…` shared `kevin@…`'s account and received its codes | `NormalizeEmail` refuses an address with a non-ASCII character that lowercasing changes (`invalid_email`) | Every address accepted before normalizes exactly as before, so stored `email_normalized` values stay valid; only uppercase or compatibility non-ASCII characters are refused. NFKC was not needed: without case folding, compatibility characters such as fullwidth letters are simply different addresses |
| AUTH-S-4 timing (Low) | Existing accounts did more database work on forgot-password, resend and register | The three take at least `auth.DefaultMinResponseTime` (300 ms, `Config.MinResponseTime`), measured from after input validation | Moving the work to a job would put email addresses into job arguments visible in `/ops/jobs` and still leave register's account hooks in the request. A floor well above the work hides the difference as long as the database answers in time; `TestAnonymousFlowsTakeTheSameTime` checks each outcome's median is at the floor and within a quarter of it of the others |
| AUTH-S-5 password checks behind a session (Low) | Only the per-IP limit | `auth.reauth_attempts` (10 per `auth.login_window`) per user, charged by every change that checks the password or a second factor: password change, authenticator app setup and turning it off, passkey registration and removal, linking and unlinking identities, account deletion. Wrong answers and refusals are audited as `auth.reauth.failed` | Charging per call rather than per failure needs no new limiter API and costs legitimate users nothing at these volumes |
| AUTH-S-6 login lockout (Low) | 10 attempts per address | `auth.login_attempts` per address and client network (`ratelimit.ClientKey`: IPv4 address or IPv6 /64), then `auth.login_address_attempts` (50) per address from any network; second factors count toward both | Someone else needs 50 wrong passwords in 15 minutes, not 10, to block the owner, while stuffing from many networks stays bounded |
| AUTH-S-7 IPv6 (Medium, = OPS-1) | Each IPv6 address had its own per-IP budget | `ratelimit.ByRemoteIP` keys IPv6 by /64 (core); the app's per-IP limit uses it | A /64 is what one host or customer gets |
| AUTH-S-8 argon2 work (Info) | Unbounded wait for a hashing slot; reset hashed before checking the code | `Hasher` waits for a slot until the request's context ends or `DefaultHashMaxWait` (5 s) passes and returns `ErrHasherBusy` (503 `auth_unavailable`); `WithHashConcurrency` and `WithHashMaxWait` via `NewHasherWith`; `ResetPassword` hashes only after the code matched, and `ChangePassword` after the current password did | Context-aware methods (`HashContext`, `VerifyContext`, `VerifyDummyContext`) are additions, so the frozen API is unchanged; the old methods wait at most the maximum wait |

| Check | Result |
|---|---|
| `TestPreRegistrationTakeover` (the reviewers' PoC, attacker first, within and after the resend interval) | The attacker's password fails after the owner verifies |
| `TestRegisterAgainKeepsOnlyTheSamePassword`, `TestVerifyingRemovesWhatCameBefore` | Same password kept; another removes it, reset sets it; verification by code or reset removes an operator-enrolled authenticator app |
| `TestCodeChecksAreLimitedAcrossCodes` | 20 wrong reset codes across 4 codes, then 429 even for the right code; other and unknown addresses have their own budget; refills after the window |
| `TestNormalizeEmail` cases in `modules/auth`, `TestRegisterValidatesInput` | Kelvin, Angstrom and Ohm signs and uppercase non-ASCII refused; lowercase non-ASCII accepted |
| `TestAnonymousFlowsTakeTheSameTime` | Forgot password, resend and register: every outcome at the floor, within a quarter of it, under the race detector |
| `TestChecksBehindASessionAreLimited` (use cases and HTTP, the reviewers' PoC) | 10 wrong passwords across the five changes, then 429 even for the right password, from a new IP each time |
| `TestLoginLimitIsPerNetwork`, `TestPerIPLimitGroupsIPv6`, `TestByRemoteIPGroupsIPv6By64` | Another network signs in after a /64 is limited; the address-wide limit still applies; a /64 shares the per-IP budget |
| `TestHasherWaitIsBounded`, `TestResetChecksTheCodeBeforeHashing` | A busy hasher returns `ErrHasherBusy` after the wait or when the context ends; wrong reset codes never wait for it |

### Follow-up (2026-09-16)

Three items left open by the fixes above, closed after review:

- **Roles only for verified accounts.** `GrantRole` returns `ErrEmailNotVerified` for an account whose address isn't verified, and `grant-role` says so ("hasn't verified its email address; roles are granted only to verified accounts"). It used to grant the role with a note. Verification removes what came before (`claimAddress`), but a role would survive it and reach whoever verifies. Seed data and `CreateUser` create verified accounts, so no other path is affected.
- **Unverified accounts expire.** `Cleanup` (the `auth_cleanup` job) soft-deletes accounts still unverified after `auth.unverified_account_ttl` (default 7 days, 1 hour – 90 days, reason required; library hard limits `auth.UnverifiedAccountLimits`), 500 per statement and at most 20 statements per run (`FOR UPDATE SKIP LOCKED`), runs the `AccountDeleted` hook for each and records one `auth.accounts.unverified_expired` event with only the count. Accounts with a Google or Apple identity, and accounts sent a code within the TTL, are kept. Soft deletion reuses account deletion: the address is free at once and the rows go after `auth.deleted_account_retention`. No `CheckAccountDeletion`: an unverified account without an identity can't sign in, so it owns nothing to hand over. `/ops/retention` lists `unverified_accounts`.
- **A request signed in to the account claims nothing from itself.** `claimAddress` removes nothing when the verifying request is signed in to that same account (ADR-0046 follow-up): its session came from the account's own identity, and the code proves the address too.

| Check | Result |
|---|---|
| `TestRolesGrantPermissions`, `TestAuthenticationEndToEnd` | An unverified account gets `ErrEmailNotVerified` and no role; `grant-role` explains why |
| `TestCleanupExpiresUnverifiedAccounts` | Of accounts registered 49 hours earlier with a 48-hour TTL, the one without a recent code is deleted and its address can register again; a verified account, a Google account without a verified address, one sent a code two hours earlier and a recent registration stay; one audit event with only the count |
| `TestOpsRetention` | `unverified_accounts` names `auth.unverified_account_ttl` and `auth_cleanup` |
