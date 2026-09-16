# ADR-0043: Two-factor authentication

**Status:** Accepted (2026-09-15) · **Amends:** ADR-0024, ADR-0038, ADR-0042 · **Amended by:** ADR-0044 (a passkey also turns two-factor authentication on), ADR-0046 (Google and Apple sign-ins also ask for the second factor; accounts without a password confirm with a recent sign-in), ADR-0053 (per-user limit on checks behind a session, 80-bit recovery codes)

## Context

v0.3 (roadmap) adds strong authentication. ADR-0024 fixed the outline for 2FA: a TOTP secret encrypted at rest with a rotatable key, 10 hashed recovery codes, single-use rate-limited codes, an `mfa_required` challenge before a session exists, roles that require 2FA, and ops roles that always do. It named `github.com/pquerna/otp` for TOTP.

v0.2 left what this needs: sessions are created only at login (ADR-0038), permissions come from roles on every request, `/ops/*` checks permissions in its use cases, and seed data creates a `platform_admin` (ADR-0042). Threat model rows 15 (TOTP secret disclosure or replay) and 18 (privilege escalation to ops, whose required 2FA is still open) depend on this decision.

This ADR covers TOTP, recovery codes, the login challenge and the role policy. Passkeys and Google and Apple sign-in get their own ADRs; the challenge and session model here is built so a passkey can count as a second factor.

## Options

### TOTP implementation

| | 1. `pquerna/otp` | 2. RFC 6238 in `modules/auth` |
|---|---|---|
| Size | A dependency with QR image generation we don't need | About 60 lines: HMAC-SHA1, dynamic truncation, time steps |
| Maintenance | Last release v1.5.0, December 2024 | Ours; checked against the RFC 6238 test vectors |
| Dependency budget | +1 module and its transitive imports | None |

### When 2FA is required in development

| | 1. Required everywhere; seed enrolls the administrator | 2. Required everywhere; developers enroll | 3. Required only in production |
|---|---|---|---|
| First run | `/ops/*` works after adding a printed key to an authenticator | Sign in, enroll through the API, then `/ops/*` | Works at once |
| Same rule in every environment | Yes | Yes | No: tests and behaviour split by `APP_ENV` |

## Decision

TOTP option 2; development option 1 (chosen 2026-09-15).

### Who owns what (as ADR-0038)

| Library: `gorbital.dev/modules/auth` | App: `internal/modules/auth` |
|---|---|
| `TOTP`: RFC 6238 with SHA-1, 6 digits, 30-second steps, ±1 step of clock skew; `NewTOTPSecret` (20 random bytes, base32), `TOTPURI` (`otpauth://totp/...` for authenticator apps), `VerifyTOTP` returning the matched step | Enrollment, confirmation, disabling, challenges, recovery codes, policy checks, tables, endpoints |
| `Keyring`: AES-256-GCM with key IDs; encrypts with the first key, decrypts with any; additional data binds a ciphertext to its user and purpose | Stores ciphertexts; the `rotate-auth-keys` command |
| `NewRecoveryCodes` (10 codes of 10 base32 characters, shown as `xxxxx-xxxxx`), `HashRecoveryCode` | `auth_recovery_codes` |
| `Catalog.RequireMFA(roles...)` | `internal/app/permissions.go` marks `platform_admin` and `ops_viewer` |

No QR images are generated: the API returns the `otpauth://` URI and clients render it.

### Encryption keys

| Topic | Decision |
|---|---|
| Variable | `AUTH_ENCRYPTION_KEYS` (a `config.Secret`): comma-separated `id:base64key` entries, each a 32-byte key; the first encrypts |
| Stored form | Key ID, nonce and ciphertext; additional data is `user_id` and `totp` so a ciphertext copied to another row fails to decrypt |
| Rotation | Put a new key first and keep the old one; `go run ./cmd/api rotate-auth-keys` re-encrypts every secret with the first key and is audited; then remove the old key |
| Production | The app refuses to start without a valid key |
| Development | `orb dev` writes a random key to `.env` when the variable is empty; without a key (plain `go run`), 2FA endpoints return 503 `mfa_unavailable` and users with 2FA can't sign in, with a log line saying why |
| `.env.example` | The variable, empty, with how to generate a key (`openssl rand -base64 32`) |

### Tables

| Table | Columns (beyond IDs and times) |
|---|---|
| `auth_totp` | `user_id` (primary key), `key_id`, `secret_ciphertext`, `confirmed_at` (NULL while enrolling), `last_used_step` (replay guard) |
| `auth_recovery_codes` | `user_id`, `code_hash` (SHA-256; codes carry 50 random bits), `used_at` |
| `auth_mfa_challenges` | `user_id`, `token_hash`, `attempts`, `max_attempts` (5), `expires_at` (5 minutes), `consumed_at`, `ip`, `user_agent` |
| `auth_sessions` | new `mfa_verified_at`: set when the session was created with a second factor or later confirmed one |

### Enrollment

| Endpoint | Behaviour |
|---|---|
| `POST /v1/auth/mfa/totp` | Signed in; body carries the current password. Stores a new unconfirmed secret (replacing an unconfirmed one) and returns the base32 secret and `otpauth://` URI, once. 409 `mfa_already_enabled` when confirmed |
| `POST /v1/auth/mfa/totp/confirm` | Body carries a code. Confirms the secret, marks the current session 2FA-verified, ends the user's other sessions, returns 10 recovery codes once, sends a "2FA turned on" email |
| `DELETE /v1/auth/mfa/totp` | Body carries the password and a code or recovery code. Deletes the secret and recovery codes, ends other sessions, sends a "2FA turned off" email. A user whose roles require 2FA can't turn it off (409 `mfa_required_by_role`) |
| `POST /v1/auth/mfa/recovery-codes` | Body carries a code. Replaces all recovery codes and returns the new ones once |
| `GET /v1/auth/me` | Adds `mfa_enabled`, `session_mfa_verified` and `mfa_required` (a role requires it) |

### Signing in

1. `POST /v1/auth/login` checks the password as today. For a user with confirmed TOTP it creates no session and returns **202** with `challenge_token` (32 random bytes; only its hash is stored), `methods` (`totp`, `recovery_code`) and `expires_at`. Users without 2FA get 200 and a session, unchanged.
2. `POST /v1/auth/login/mfa` takes `challenge_token`, one of `code` or `recovery_code`, and `transport`. It returns 200 with a 2FA-verified session, like login. Wrong or expired challenges and wrong codes return 401 `invalid_mfa` with the same work; 5 attempts per challenge; the per-address login limiter also counts these attempts.
3. A TOTP code is accepted only for a step later than `last_used_step`, updated in the same statement that checks it, so a code can't be used twice, even by two instances at once. A used recovery code is marked in the same way and sends a "recovery code used" email naming how many are left.

Password reset doesn't bypass 2FA: it sets the password and ends sessions, and the next sign-in still asks for a code. Account deletion with 2FA also requires a code or recovery code.

Lost device and recovery codes: an operator runs `go run ./cmd/api reset-mfa <email>`, audited as the `cli` actor, which removes the user's 2FA and ends their sessions.

### Role policy

| Topic | Decision |
|---|---|
| Declaring | `catalog.RequireMFA("platform_admin", "ops_viewer")` in `internal/app/permissions.go`; roles not listed don't require 2FA |
| Enforcing | `Authenticate` grants a role's permissions only when that role doesn't require 2FA or the session is 2FA-verified. Permissions withheld this way are listed on the actor, so a check can tell "not allowed" from "sign in with 2FA first" |
| Core addition | `actor.Actor.StepUp []string` (permissions a 2FA-verified session would grant) and `actor.Require(ctx, permission) error`, returning `actor.ErrForbidden` or `actor.ErrStepUpRequired`; ops use cases switch to it |
| HTTP | `actor.ErrStepUpRequired` maps to 403 `mfa_required`: enroll 2FA, or sign in again with a code |
| Existing sessions | Sessions created before an app upgrades aren't 2FA-verified; ops users sign in again after enrolling |
| Runtime settings | None: which roles require 2FA is code, so `/ops/settings` can't weaken it (threat 23) |

### Seed data (amends ADR-0042)

Seed also enrolls the administrator: it creates and confirms a TOTP secret and recovery codes through the use cases, and prints, once, next to the password, the setup key, the `otpauth://` URI and the 10 recovery codes. Nothing is stored in plain text. The first `orb dev` still ends with a working `/ops/*`: add the key to an authenticator app, sign in, answer the challenge.

### Audit actions and emails

`auth.mfa.totp_enrollment_started`, `auth.mfa.totp_enabled`, `auth.mfa.totp_disabled`, `auth.mfa.challenge_succeeded`, `auth.mfa.challenge_failed` (reason), `auth.mfa.recovery_code_used`, `auth.mfa.recovery_codes_regenerated`, `auth.mfa.reset` (operator), `auth.keys.rotated`. Secrets, codes and recovery codes never appear in metadata or logs. Emails: 2FA turned on, 2FA turned off, recovery code used.

### Error codes

`mfa_required` (403), `invalid_mfa` (401), `mfa_already_enabled` (409), `mfa_not_enabled` (409), `mfa_required_by_role` (409), `mfa_unavailable` (503). Login's 202 response is a new success shape, so existing clients of apps without 2FA users see no change.

## Why

- Codes checked against a stored step and challenges stored as hashes keep a leaked database or a replayed code from signing anyone in.
- Encryption with key IDs lets a key be replaced without a flag day, and binding ciphertexts to users stops secret swapping between rows.
- Withholding permissions per session, rather than blocking sign-in, lets ops users enroll with the same account and gives clients a clear `mfa_required`.
- Seed enrollment keeps one rule for every environment without slowing the first run.

## Trade-offs

- An app now needs a managed encryption key; losing every key disables everyone's 2FA until operators reset it.
- Upgrading apps' ops users lose `/ops/*` until they enroll and sign in again.
- TOTP is phishable; passkeys (next ADR) are the phishing-resistant factor.
- Printing a TOTP key and recovery codes to a development terminal puts them in scrollback, like the seeded password.
- Per-instance rate limits (ADR-0038) still apply; the replay guard and challenge attempts are database-backed and shared.

## Consequences

- `modules/auth` gains `TOTP`, `Keyring`, recovery code helpers and `Catalog.RequireMFA`; core `actor` gains `StepUp`, `Require`, `ErrForbidden` and `ErrStepUpRequired` (public API, ADR-0015).
- The Full preset gains a migration, use cases, repository files and endpoints, `reset-mfa` and `rotate-auth-keys` commands, seed enrollment, and tests for replay, challenge limits, key rotation, role policy and seed.
- `orb dev` writes `AUTH_ENCRYPTION_KEYS` to `.env` when empty.
- Threat model rows 15 and 18 (required 2FA for ops) are addressed when this ships; ADR-0024's library list drops `pquerna/otp`.

## Implementation notes (2026-09-15)

- `actor.Require` also returns `actor.ErrUnauthenticated`, so a use case maps three outcomes. The ops module maps `ErrStepUpRequired` to its own `ErrMFARequired` (403 `mfa_required`), keeping its domain free of core imports.
- Login's 202 body carries only `mfa`: no user or session, so a password alone reveals nothing more about the account. `user` and `session` became optional fields of the login response.
- Attempts on `POST /v1/auth/login/mfa` count toward the per-address login limit; confirming, turning off and replacing recovery codes share a per-user limit.
- `EnrollTOTP` (operators, seed data) confirms a secret without a code and records step 0, so the first real code is accepted.
- `auth_cleanup` removes sign-in challenges a day after they expire.
- `LoadConfig` validates `AUTH_ENCRYPTION_KEYS` everywhere and requires it in production. Tests share one generated key per process, so two app instances on one database decrypt each other's secrets.
- `orb dev` fills an empty `AUTH_ENCRYPTION_KEYS` in `.env` with `dev:<random key>`, only for apps whose `.env.example` declares it and when the environment doesn't set it, and keeps `.env` at mode 0600.
- The migration is `20260915000004_auth_mfa.sql`.
- Amended 2026-09-15 (with ADR-0044): the setup response also returns `qr_code`, a PNG data URL of the `otpauth://` URI, so the API is usable with an authenticator app before any frontend exists. `modules/auth` gains `TOTPQRCode` on `rsc.io/qr` (no dependencies of its own); the "no QR images" line above is replaced.

| Check | Result |
|---|---|
| Library (`modules/auth`) | RFC 6238 test vectors; the ±1 step window; keyring round trip, tampering, additional-data binding, rotation and parse errors; recovery code format and normalization; `RequireMFA` and `PermissionsFor` |
| Core (`actor`) | `Require` for no actor, anonymous, granted, step-up and forbidden |
| Use cases (Docker PostgreSQL) | Encrypted storage; enrollment and confirmation; a used step refused; challenge success, exhaustion after 5 attempts and expiry; recovery codes used once with the remaining count emailed; role policy with step-up permissions; turning off and replacing codes; account deletion needing a second factor; operator enrollment and reset; key rotation with the new key alone finishing a sign-in; no keys |
| HTTP | 403 `mfa_required` on `/ops/*` before 2FA; `/v1/auth/me` fields; setup and confirmation; 202 challenge with nothing else; a used code refused; recovery code sign-in; `mfa_required_by_role`; the audit event |
| Seed | The printed password and 2FA key sign in and `/ops/*` answers; a second run prints no secrets |
| Every existing test | Passes with ops helpers signing in through the challenge |
| New Full app (`ORB_E2E=1`) | Passes its own suite, including the tests above |
| `orb dev` with Docker (`ORB_E2E_DOCKER=1`) | Writes a development `AUTH_ENCRYPTION_KEYS`; seed enrolls the administrator; the printed password and a code from the printed key sign in; API ready in 9 s with warm caches |
| Lint | golangci-lint clean in `actor`, `modules/auth`, `examples/full-single` and `cli`; no new module dependencies |

## Security review fixes (2026-09-16)

- **Recovery codes (AUTH-M-4):** new codes carry 80 random bits, 16 base32 characters shown as `xxxx-xxxx-xxxx-xxxx`, instead of 50 bits in 10 characters; `HashRecoveryCode` stays `SHA-256(user_id:normalized code)`, so codes made before keep working until replaced. An unkeyed hash of 50 bits could be reversed from a database dump in GPU-hours; 80 bits take about 10^13 GPU-seconds.
- The triage proposed an HMAC keyed from `AUTH_ENCRYPTION_KEYS`. Rejected: a recovery code can't be re-hashed without the code, so `rotate-auth-keys` couldn't move codes to a new key, and removing the old key after rotation, as the key guide says to, would silently break the codes of every user, the one way back for someone who lost their authenticator app. Passkey-only accounts also have recovery codes on servers without encryption keys. More entropy protects every code with no key to manage.
- **Checks behind a session (AUTH-M-2):** setting up and turning off the authenticator app, and account deletion's second factor, also spend the per-user `auth.reauth_attempts` budget (ADR-0038 security review fixes).

| Check | Result |
|---|---|
| `TestRecoveryCodes` | 10 distinct codes of 16 base32 characters in four groups; a 10-character code still hashes like its variants |
| `TestChecksBehindASessionAreLimited` | Wrong passwords through authenticator app setup and deletion count toward one per-user limit |
