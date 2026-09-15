# ADR-0046: Google and Apple sign-in

**Status:** Accepted (2026-09-15) · **Amends:** ADR-0024, ADR-0043, ADR-0045

## Context

ADR-0024 plans Google and Apple sign-in for v0.3: OIDC authorization code with PKCE, state and nonce on the web; ID-token verification for native apps; Apple's client secret signed from the `.p8` key; account linking only on a verified email match; `golang.org/x/oauth2` and `github.com/coreos/go-oidc`. ADR-0045 fixed the environment variables, the callback URLs (`/v1/auth/{google,apple}/callback`) and how developers are told what to provide.

Open questions left: how the web flow returns to a separate frontend, how native apps prove freshness, what happens to an existing account with the same email, whether a second factor still applies, how accounts without a password confirm sensitive changes, and what Apple requires on account deletion.

The maintainer decided (2026-09-15): link automatically on a provider-verified email match, and keep asking for the second factor on accounts that have one.

## Decision

### 1. Library: `apistock.dev/modules/auth/social`

| Library provides | Generated app owns |
|---|---|
| `social.Google` and `social.Apple` providers: authorization URL (state, nonce, PKCE S256), code exchange, ID-token verification (issuer, audience list, expiry with 1-minute skew, nonce) with cached JWKS | Flows, account resolution and linking, tables, endpoints, emails, audit |
| `social.Identity{Provider, Subject, Email, EmailVerified, PrivateEmail, Name}` | Where identities are stored and how they map to users |
| Apple client secret: an ES256 JWT (`iss` team, `sub` Services ID or bundle ID, `aud` `https://appleid.apple.com`) signed for 5 minutes per request; token revocation (`/auth/revoke`) | Calling revocation on account deletion and unlinking |
| Apple server-to-server notification verification | Acting on notifications |
| `socialtest`: an in-process OIDC provider (discovery, JWKS, token endpoint, signed ID tokens) for tests | Tests over HTTP without Google or Apple |

Dependencies: `golang.org/x/oauth2` (v0.37), `github.com/coreos/go-oidc/v3` (v3.21), and `github.com/golang-jwt/jwt/v5` (already in the graph through passkeys). Separate package, so apps that don't import it don't compile it.

### 2. Configuration

ADR-0045's variables, plus one:

| Variable | Meaning | Development default |
|---|---|---|
| `APP_PUBLIC_URL` | The API's public base URL, used to build callback URLs | `http://localhost:8080` |

- Google is on when `GOOGLE_CLIENT_ID` and `GOOGLE_CLIENT_SECRET` are set; `GOOGLE_IOS_CLIENT_ID` and `GOOGLE_ANDROID_CLIENT_ID` add native audiences. A native ID without the web client is refused at start.
- Apple web is on with `APPLE_TEAM_ID`, `APPLE_SERVICES_ID`, `APPLE_KEY_ID` and the key (`APPLE_PRIVATE_KEY_FILE` or `APPLE_PRIVATE_KEY`); `APPLE_BUNDLE_IDS` adds native audiences and needs the team, key ID and key.
- Partial configuration, an unreadable or non-P-256 key, and `APP_PUBLIC_URL` without https in production stop the app at start, naming the variable (ADR-0045).
- The status block, `auth-providers` and `GET /ops/auth/providers` gain `google`, `google_ios`, `google_android`, `apple` and `apple_ios`.

### 3. Web flow

```text
Browser → GET /v1/auth/{provider}/start?return_to=https://app.example.com/after-login
        ← 302 to Google or Apple, with a __Host-oauth cookie
Provider → GET /v1/auth/google/callback?code&state   or   POST /v1/auth/apple/callback (form_post)
        ← 303 to return_to: session cookie set, or #mfa_challenge_token=…&methods=… , or #error=…
```

| Topic | Decision |
|---|---|
| State | A `auth_oauth_states` row: token hash, provider, nonce, PKCE verifier, `return_to`, 10-minute expiry, single use |
| Browser binding | The `__Host-oauth` cookie (HttpOnly, Secure, `SameSite=None` so Apple's cross-site form post carries it) holds a random value whose hash the state row stores; the callback requires it, so a stranger's link can't sign someone into the stranger's account |
| PKCE | S256 for Google; Apple uses the signed client secret with state and nonce |
| `return_to` | Must be on an origin in `APP_CORS_ORIGINS` or the API's own origin; otherwise 422 `invalid_return_to` before redirecting. Default: the API's `/docs` in development |
| Result | Success sets the session cookie like `POST /v1/auth/login` and redirects. A second factor redirects with `#mfa_challenge_token=…&methods=…` in the fragment (never sent to servers), finished with `POST /v1/auth/login/mfa`. Errors redirect with `#error=<code>` |
| Apple's name | Sent only on the first authorization; stored on the identity |

### 4. Native flow

| Endpoint | Behaviour |
|---|---|
| `POST /v1/auth/{provider}/nonce` | Public. Returns a single-use `nonce` (5 minutes, stored hashed) to put in the SDK request |
| `POST /v1/auth/google/token` | `{id_token, nonce, transport}`: audience must be one of the configured Google client IDs (web, iOS, Android) |
| `POST /v1/auth/apple/token` | `{id_token, nonce, authorization_code?, name?, transport}`: audience must be in `APPLE_BUNDLE_IDS`; Apple's token carries the SHA-256 of the nonce. `authorization_code` is exchanged for a refresh token kept for revocation |

Both return 200 with a session, or 202 with a second-factor challenge, exactly like `POST /v1/auth/login`.

### 5. Accounts

```text
identity (provider, subject) known       → its user
else provider-verified email matches user → link to that user
else provider-verified email              → new user, email verified, no password
else                                      → 403 social_email_unverified
```

| Topic | Decision |
|---|---|
| Table | `auth_identities`: `user_id`, `provider`, `subject` (unique with provider), `email`, `private_email`, `name`, encrypted Apple refresh token (`AUTH_ENCRYPTION_KEYS`), `created_at`, `last_used_at` |
| Verified email | Google: `email_verified` true. Apple: always verified, including relay addresses |
| Linking an existing account | Automatic on a verified match. If the existing account never verified its email, it is marked verified, its **password is removed and its sessions end**: someone who registered the address without owning it loses access (pre-account-hijacking). Emails "Google sign-in added" or "Apple sign-in added" |
| Email changes | A later sign-in with a different provider email doesn't change the account's email |
| Deleted accounts | Their identities are deleted; signing in again creates a new account |
| Relay emails | Accepted as the account email; delivery needs the sender registered with Apple (AUTH_PROVIDERS.md) |

### 6. Second factor and step-up

- A Google or Apple sign-in to an account with two-factor authentication returns a challenge, like a password sign-in; the session is verified only after the second factor.
- Accounts without a password confirm sensitive changes (starting authenticator app setup, deleting the account, turning 2FA off, adding or removing a passkey after the 10-minute window) by signing in with their provider again: the session records `authenticated_at`, and a sign-in less than 10 minutes old stands in for the password. Setting a password through the reset flow is always possible.

### 7. Managing identities

| Endpoint | Behaviour |
|---|---|
| `GET /v1/auth/identities` | The user's linked providers: provider, email, private email, created, last used |
| `DELETE /v1/auth/identities/{id}` | Unlinks, after the password or a recent sign-in; 409 `last_sign_in_method` when no password, passkey or other identity remains; queues Apple's token for revocation |
| `POST /v1/auth/apple/notifications` | Apple's server-to-server events, verified with Apple's keys: `consent-revoked` and `account-delete` unlink, and end the account's sessions when no other way to sign in remains; `email-disabled`/`email-enabled` recorded |

Account deletion revokes stored Apple tokens, as App Store rules require, through a background job with retries (implementation notes).

### 8. Security and limits

- The per-IP auth limit extends to `GET /v1/auth/{provider}/start` and the callbacks; token endpoints count toward it like login.
- ID tokens older than 10 minutes (`iat`) are refused; nonces and states are single use and deleted by `auth_cleanup` once expired.
- Provider errors never leak tokens or codes to logs; audit records provider and subject hash only.

### Audit, emails and errors

Audit: `auth.login.succeeded` (`method: google|apple`), `auth.login.failed` (reasons `invalid_social_token`, `social_email_unverified`, `invalid_state`), `auth.identity.linked`, `auth.identity.unlinked`, `auth.identity.apple_notification`. Emails: provider sign-in added, provider sign-in removed.

Errors: `invalid_social_token` (401), `invalid_state` (401), `social_email_unverified` (403), `last_sign_in_method` (409), `invalid_return_to` (422), `social_unavailable` (503).

## Why

- Standard OIDC with a maintained verifier avoids hand-written JWT validation and vendor SDKs on the server.
- Server-side state bound to a cookie defeats login CSRF; fragments keep challenge tokens out of server logs and `Referer`.
- Automatic linking matches what users expect, and removing an unverified account's password closes the pre-hijacking hole automatic linking otherwise opens.
- Keeping the second factor makes 2FA policy independent of each provider's own security.
- Revoking Apple tokens and handling Apple's notifications meets App Store rules without a separate job.

## Trade-offs

- `SameSite=None` on the short-lived state cookie is needed for Apple's form post.
- Apple's web flow can't be tried on localhost; development uses a tunnel or the native flow.
- Relying on provider email verification means a provider mistake could link the wrong account; unverified Google emails are refused.
- One more table, a library package, and seven endpoints.

## Consequences

- `modules/auth/social` and `socialtest` (public API, ADR-0015).
- The Full preset gains a migration, repository files, use cases, endpoints, emails, `.env.example` blocks, status lines and tests for both flows, linking (including the unverified-account case), the second factor, unlinking, deletion revocation and Apple notifications.
- ADR-0045 gains `APP_PUBLIC_URL`; `AUTH_PROVIDERS.md` loses "coming in a later version".
- Threat model rows 14–15 are reviewed when this ships.

## Implementation notes (2026-09-15)

- `modules/auth/social` verifies tokens with go-oidc's remote key set and checks the issuer list, audience list, nonce (constant time) and age itself; Apple's ES256 client secret is signed with go-jose, already in the graph through go-oidc. New modules: `golang.org/x/oauth2` v0.37, `github.com/coreos/go-oidc/v3` v3.21, `github.com/go-jose/go-jose/v4`.
- "A recent sign-in" is the session's start: `auth.Principal.SignedInAt`, from `auth_sessions.created_at`, within `auth.RecentVerification` (10 minutes). No new column. It replaces the password for accounts without one in authenticator app setup, turning it off, account deletion, passkey changes and unlinking.
- A consent-revoked or account-delete notification unlinks the Apple identity and ends the account's sessions only when no password, passkey or other identity remains: sessions don't record which method started them.
- Cross-origin protection skips `POST /v1/auth/apple/callback` and `POST /v1/auth/apple/notifications`; the per-IP auth limit covers `GET …/start` and `…/callback`.
- A web sign-in without the `__Host-oauth` cookie still uses up its state and returns to its `return_to` with `#error=invalid_state`; a person cancelling at the provider returns `#error=access_denied`.
- Account deletion unlinks identities in its transaction and queues Apple tokens for revocation (below). `rotate-auth-keys` re-encrypts Apple refresh tokens with the authenticator secrets.
- User responses gain `has_password`, so clients can hide "change password" for accounts created with Google or Apple.
- Two first sign-ins of one person at the same moment race to create the account or identity; the loser's transaction rolls back on the unique constraint (`ErrEmailTaken`, `ErrIdentityTaken`) and retries once, finding the winner's account. Tested with concurrent requests under the race detector.
- Apple token revocation is a background job (2026-09-15, after review): unlinking an identity or deleting an account writes the still-encrypted token to `auth_token_revocations` in the same transaction, so no token is lost and the request never waits on Apple. The `auth_revoke_tokens` job (every minute) claims up to 20 due tokens with `FOR UPDATE SKIP LOCKED` and a 10-minute lease, revokes them, retries failures after 1, 2, 4 … minutes up to 6 hours, and after 10 attempts deletes the row, logs an error and records `auth.identity.revocation_abandoned`. `rotate-auth-keys` re-encrypts queued tokens too. Apple's own consent-revoked and account-delete notifications queue nothing: Apple has already revoked them.
- The status block, `auth-providers` and `GET /ops/auth/providers` report `google`, `google_ios`, `google_android`, `apple` and `apple_ios`, with the callback URL.

| Check | Result |
|---|---|
| Library (`socialtest` fake provider) | Google authorization URL with PKCE; code exchange with verifier and secret; used codes and other nonces refused; native audiences; string booleans; wrong audience, nonce, issuer, age, expiry, subject, signature and key refused; Apple form_post URL, ES256 client secret claims and key ID, native code exchange, revocation, notifications with audience checks; configuration and `.p8` parsing |
| Use cases (Docker PostgreSQL) | New account, returning identity, Apple linking by email with an email, verified password account kept, unverified account's password removed; invalid return addresses, other browser, other provider, expired and used states, other nonce, unverified email, cancelled sign-in; second factor asked; native Google and Apple with single-use and hashed nonces, audience and refresh token; last sign-in method kept, unlinking after a recent sign-in, deletion queuing Apple's token and the job revoking it, a new account afterwards; revocation retried with backoff and abandoned with an audit event; Apple notifications |
| HTTP | Google redirect with the SameSite=None cookie, callback setting the session and redirecting, missing cookie and bad `return_to`; Apple's cross-site form post with the name; identities; native token with nonce reuse refused; notifications accepted and forged ones refused; configuration errors and status lines |
