# ADR-0024: Authentication methods

**Status:** Accepted (2026-09-14) · **Supersedes:** ADR-0006 · **Amended by:** ADR-0038 (flows, tables and SQL live in the generated app's `internal/modules/auth`; `modules/auth` provides building blocks)

## Context

The Full preset promises complete authentication on first run. Authentication is the highest-risk area: it must be secure by default, support web and native mobile clients, and receive security fixes through the library rather than copied code.

## Options

1. JWT access + refresh tokens, social login through a vendor SDK (for example Firebase).
2. External identity service (Ory, Keycloak, Auth0) required for every app.
3. Embedded library with server-side sessions and standards-based social login, 2FA and passkeys; owned HTTP handlers in the app.

## Decision

Option 3. Logic lives in `apistock.dev/modules/auth`; the generated `internal/modules/auth` holds endpoints, customisable rules and adapters.

### Methods and flows (Full preset)

| Capability | Design | Milestone |
|---|---|---|
| Email + password | argon2id with stored parameters, rehash on login; minimum length 12; optional breached-password check hook | v0.2 |
| Email verification | 6-digit code, hashed, 15-minute expiry, attempt-limited, resend throttled | v0.2 |
| Sessions | Opaque 32-byte token; only SHA-256 stored; `__Host-` cookie (Secure, HttpOnly, SameSite=Lax) for browsers or `Authorization: Bearer` for native clients; idle and absolute expiry; rotation on login and privilege change | v0.2 |
| Logout / logout all | Revoke one or all sessions | v0.2 |
| Password reset | Single-use hashed code or token, 30-minute expiry, identical response whether or not the email exists, all sessions revoked on success | v0.2 |
| Change password | Requires the current password; other sessions revoked | v0.2 |
| Active sessions | List devices; revoke one | v0.2 |
| Delete account | Soft delete, sessions revoked, purge job after retention period | v0.2 |
| Platform roles and permission catalog | Modules declare permissions; roles are sets of permissions; deny by default | v0.2 |
| Google sign-in | OIDC authorization code + PKCE with state and nonce (web); ID-token verification (native) | v0.3 |
| Apple sign-in | Same two flows; client secret signed per request from the `.p8` key; name stored on first sign-in; private relay emails supported | v0.3 |
| Account linking | Only on a verified email match | v0.3 |
| TOTP 2FA | Secret encrypted at rest with a rotatable app key; 10 hashed recovery codes; codes rate-limited and single-use; `mfa_required` challenge before a session is created | v0.3 |
| Passkeys | WebAuthn registration and login; relying-party ID and allowed origins configured; counts as strong 2FA; `apple-app-site-association` and `assetlinks.json` served | v0.3 |
| 2FA policy | Roles can require 2FA; ops roles require it | v0.3 |
| GitHub login, API keys | Custom option / v1.1 | v1.1 |
| SAML, SCIM, enterprise SSO | Delegated to identity providers | Later |

Social providers are included but disabled until credentials are configured; `aps dev` reports their status.

### Security rules

- No account enumeration on register, login or reset responses.
- CSRF protection for cookie-authenticated routes (`http.CrossOriginProtection`).
- Per-account and per-IP rate limits; no hard lockout.
- Security events recorded through `audit.Recorder` (login succeeded/failed, MFA enrolled, passkey added, password changed, session revoked).
- A new sign-in alert email for unrecognised devices.

### Libraries

`golang.org/x/crypto/argon2`, `golang.org/x/oauth2`, `github.com/coreos/go-oidc`, `github.com/pquerna/otp`, `github.com/go-webauthn/webauthn`.

## Why

- Revocable sessions are simpler and safer than JWT session tokens.
- Standards-based providers avoid vendor SDK lock-in.
- Security fixes ship through `go get`.

## Trade-offs

- A large security surface to maintain; an external security review is required before 1.0.
- Apple web sign-in can't use `localhost` redirects; local testing uses the native flow or a tunnel (documented).

## Consequences

- The data model for MFA challenges and role scopes is designed in v0.2 even though the features ship in v0.3–v0.4.
- `docs/auth-providers.md` in generated apps explains Google, Apple and passkey setup.
