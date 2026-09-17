# ADR-0087: Testing sign-in from the Dev Portal

**Status:** Accepted (2026-09-17) · **Builds on:** ADR-0043 (authenticator apps), ADR-0044 (passkeys), ADR-0046 (Google and Apple), ADR-0059 (GitHub), ADR-0065 (dev console), ADR-0066 (Dev Portal), ADR-0074 (mail previews), ADR-0086 (tunnel) · **Amends:** ADR-0065 (console extensions)

## Context

Phase 11 of the [v0.2 roadmap](../v0.2-roadmap.md#phase-11-dev-portal-tunnel-and-live-sign-in-tests), items 99–101. A developer puts Google, Apple or GitHub keys, a passkey relying party or `AUTH_ENCRYPTION_KEYS` in `.env` and today learns whether they work by signing in for real: a mistyped secret, a redirect URI registered with a trailing slash, an Apple key that belongs to another team or `WEBAUTHN_ORIGINS` without the port shows up as `invalid_social_token` or a closed passkey prompt, and a test account is left behind in the database. The maintainer asked for **Test now**: each method checked live against the real provider, from the Dev Portal, without creating accounts or sessions, in development only. Switching methods on and off from the portal was rejected (a toggle can lock out the only operator); a test checks a configured method instead.

What exists:

| Piece | Where |
|---|---|
| The web flows: state, nonce and PKCE stored hashed in `auth_oauth_states`, bound to the browser by `__Host-oauth`; `social.Provider.Exchange` verifies the ID token (signature, issuer, audience, nonce, age) or reads GitHub's API | `gorbital/authhttp/internal/usecase/social.go`, `modules/auth/social` |
| Native ID tokens: `social.Provider.VerifyIDToken` with a single-use nonce | same |
| Passkeys: `passkey.Service` ceremonies with state kept server-side | `modules/auth/passkey` |
| TOTP: `auth.NewTOTPSecret`, `TOTPURI`, `TOTPQRCode`, `VerifyTOTP` (one step either side) | `modules/auth/totp.go` |
| Test email: `POST /_dev/mail/preview/send?name=test` through the app's mailer | ADR-0074 |
| The dev console's guard (Host, loopback peer, no forwarding headers, token) and the portal's proxy that adds the token | ADR-0065, ADR-0086 |

## Decisions

### 1. Where tests run: dev console extensions

The tests are endpoints of the app under `/_dev/auth/test/`, served only while the dev console exists (`APP_ENV=development` with `DEV_CONSOLE_TOKEN`), behind the console's four checks. The portal reaches them through its proxy, which adds the token; the browser never holds it.

To keep sign-in's code in sign-in's package, the console gains **extensions** (`devconsole.Sources.Extensions`: a prefix under `/_dev/` and a handler; listed in the index as `extensions`) and the authenticator gets **`gorbital.AuthSetup.DevEndpoints`**, which does nothing unless the console is on. `authhttp` registers its tests there during `Setup`. New, which refuses prefixes that overlap the console's own endpoints.

### 2. Offline checks

`GET /_dev/auth/test` returns every method (google, apple, github, passkeys, authenticator_app, email) with checks that touch neither the network nor the database. Each check has a stable `code`, `status` (ok, warn, fail, skip), a `message`, a `fix`, the `variables` involved and a `link` to the console page with the fix (environment, tunnel, mail, guide). Values of secrets never appear.

| Method | Checks |
|---|---|
| Google | client ID shape (`<n>-<id>.apps.googleusercontent.com`), secret shape (`GOCSPX-`), native client IDs, `APP_PUBLIC_URL` scheme (http only on loopback), the callback URL, the port of a local `APP_PUBLIC_URL` against `APP_ADDR`, a quick tunnel's address |
| Apple | Team and Key IDs (10 characters), the `.p8` key loads **and signs an ES256 client-secret JWT that verifies with its public key**, Services ID shape and distinct from the bundle IDs, `APP_PUBLIC_URL` https on a real domain (else the live test is unavailable, linking to the Tunnel screen), bundle IDs |
| GitHub | client ID of an OAuth app (`Ov23li…`, 20 hex) or a warning for a GitHub App's (`Iv1.`, `Iv23li`), secret shape, the callback URL and port |
| Passkeys | RP ID not an IP and not a public suffix (`golang.org/x/net/publicsuffix`), each origin with `passkey.CheckOrigin` (https except localhost, on the RP ID), whether the live ceremony's origin is allowed |
| Authenticator apps | `AUTH_ENCRYPTION_KEYS` encrypts and decrypts a throwaway secret with the current key |
| Email | `MAIL_DELIVERY` and, for `provider`, whether `RESEND_API_KEY` is set |

Deviation from the brief: the clock is not checked offline (there is nothing to compare with); it is part of the network check.

### 3. Network checks

`POST /_dev/auth/test/{google|apple|github}/check` requests the provider's keys (GitHub: its API root) and compares the answer's `Date` with this computer's clock (warn from 10 s, fail from 1 minute, the tolerance of ID tokens), then sends the configured client credentials to the token endpoint with an authorization code no one issued. A provider that accepts the client refuses only the code (`invalid_grant`, GitHub `bad_verification_code`); one that doesn't answers `invalid_client` (GitHub `incorrect_client_credentials`) first. For Apple this proves the Team ID, Key ID, key and Services ID belong together without a browser. It is the same `Provider.Exchange` sign-in uses.

### 4. Live round trips for Google, Apple and GitHub

`POST /_dev/auth/test/{provider}/start` with `{"result_url"}` creates a test and returns the provider's authorization URL built by the same `Provider.AuthCodeURL`, with **the app's real callback URL** (`APP_PUBLIC_URL/v1/auth/{provider}/callback`), so redirect URI registration is really tested. The portal opens it in a popup.

The test's state, nonce and PKCE verifier are 256-bit random values kept **only in the app's memory** (`signintest.Tester`), keyed by the state's SHA-256, for 10 minutes. When the provider returns, a middleware on sign-in's routes (`Tester.Callbacks`, first in `/v1/auth/`'s route middleware) looks the state up **before sign-in sees the request**:

- not a test's state: the request continues to sign-in untouched (Apple's form body is read and put back);
- a test's state: marked used, then the code is exchanged and the identity verified by `Provider.Exchange` exactly as sign-in does (client secret or Apple's signed client secret, PKCE, JWKS, issuer, audience, nonce, age; GitHub's user and email API), the result recorded, and the browser sent (303) to `result_url#id=<id>&method=<provider>`. No cookie is set, nothing is written.

A used or expired test's state stays known for as long as its result (30 minutes), so a replayed or late return still lands on the result page instead of reaching sign-in, which would record an `invalid_state` failure.

The result is an identity summary (subject, email, email_verified, private_email, name, audience, hosted domain) and warnings (no email, email not verified, which would refuse a new account), or a precise failure: `access_denied`, `redirect_uri_mismatch`, `invalid_client`, `invalid_grant`, `invalid_request`, `id_token_signature`, `audience_mismatch`, `issuer_mismatch`, `nonce_mismatch`, `token_expired`, `clock_skew`, `provider_unreachable`, `github_api_error`, `provider_error`, `expired`, each with a fix naming the variable or the provider console field. Provider messages go through a redactor that removes JWT-shaped and long base64 runs and the configured secrets. Tokens (access, refresh, ID) are never in a result, a log line or a response.

`POST /_dev/auth/test/{google|apple}/id-token` `{id_token, nonce}` verifies a token from a mobile SDK with `Provider.VerifyIDToken` as the token endpoints do (for Apple, against the nonce's SHA-256 in hex; when the raw nonce matches instead, the fix says so). It skips only the nonce store, since the nonce didn't come from `POST /v1/auth/{provider}/nonce`.

**Deviation: the result page is the portal's, not the app's.** The brief asked the callback to return a page that posts the outcome to the portal with `window.postMessage`. The callback redirects to the portal's own page instead (`/auth/test-result/`), which:

- needs no change to the callbacks' responses: the OpenAPI document and the frozen v0.1.0 contracts stay identical (a test checks the document is byte-identical with the console on and off);
- works when the provider page cut the popup's `window.opener`: Google's and Apple's pages send `Cross-Origin-Opener-Policy`, and the app's own responses say `same-origin`, so a page on the app's origin often has no opener to post to. The portal page is on the portal's origin, tells the Authentication screen with a `BroadcastChannel` and `window.opener.postMessage(msg, location.origin)` (never `*`), and the screen also polls `GET /_dev/auth/test/results/{id}`;
- keeps identity details off the app's origin and off the tunnel: the fragment carries only the test ID, which reveals nothing without the console token.

`result_url` must be an http(s) URL on `localhost`, `127.0.0.1` or `[::1]` without user information or fragment (fuzzed), so the callback can't become an open redirect even for a caller holding the token.

Why the state isn't bound to the browser like a sign-in's: the test starts on the portal's origin (`127.0.0.1:3100`) and returns to `APP_PUBLIC_URL` (another port or the tunnel's hostname), so no cookie is shared. The binding is instead: only a request passing the console's checks with the token creates a state; the state is 256 bits, single use, 10 minutes, in the memory of the process that holds this run's token (a new `orb dev` run, with a new token, starts a new process with no tests); and what completing a test yields is a result readable only with the token. Someone who learned a state (from the provider URL in the developer's browser) and finished it with their own provider account would only make the developer's test show their identity.

### 5. Passkeys

The ceremony must run in a browser on an origin in `WEBAUTHN_ORIGINS`, and the portal's origin (`127.0.0.1:3100`) never is (browsers don't allow passkeys on IP addresses, and the relying party checks origins). The ceremony runs on the app's origin, `APP_PUBLIC_URL`, at `/_signin-test/passkey`, served only while the console is on and passkeys are configured. When that origin isn't allowed, the live test is unavailable and says why (a frontend-only origin is a valid configuration the app can't serve a page on).

`POST /_dev/auth/test/passkeys/start` returns `<APP_PUBLIC_URL>/_signin-test/passkey#ticket=<256 bits>`. The page holds nothing (strict CSP with a nonce, no-store, no referrer, frame-ancestors none); its script reads the ticket from the fragment (never sent to a server), removes it from the address bar, and drives two steps through `POST …/options` and `POST …/finish`: a registration with `BeginRegistration` (a discoverable credential with user verification, as adding a passkey does) for a throwaway user handle, then a sign-in limited to that credential with `BeginUserLogin`, each verified by `passkey.Service`. The credential lives in the test's memory until the result is recorded, then is dropped; the browser is asked to forget it with `PublicKeyCredential.signalUnknownCredential` where supported. Browser refusals are reported by name (`SecurityError` → `rp_id_mismatch`, `NotAllowedError` → `passkey_cancelled`, `NotSupportedError` → `passkey_unsupported`), verification errors by cause (`origin_not_allowed`, `rp_id_mismatch`, `passkey_invalid`). The page then sends the browser to the portal's result page.

Deviation from "nothing new is reachable through the tunnel except the callback": when `APP_PUBLIC_URL` is the tunnel's hostname, the ceremony page is reachable through it, because that is the origin the passkey must be created on. Without a ticket, which only the console hands out, the page and its endpoints do nothing (404 for its endpoints). The ticket is single use, 5 minutes.

A real authenticator stores the throwaway passkey (named "gorbital passkey test (safe to delete)") in the password manager; the server can't prevent that for a discoverable credential, and the docs say so.

### 6. Authenticator apps and email

`POST /_dev/auth/test/totp/start` returns a throwaway secret, its `otpauth://` URI and QR code (the parameters enrolment uses), kept in memory 10 minutes. `POST /_dev/auth/test/totp/verify` checks a code with `VerifyTOTP` (one 30 s step either side); a code that doesn't verify is looked up 10 minutes either way to report `clock_drift` with the difference in seconds. Five attempts; a passed test is forgotten.

Email reuses `POST /_dev/mail/preview/send?name=test&to=…` (ADR-0074): the message goes through the app's mailer, and the portal reports the delivery's acceptance.

### 7. No audit events

Tests write nothing to the database: no account, identity, session, OAuth state, nonce, passkey, TOTP secret or audit event (a test compares every `auth_*` table and `audit_events` before and after). A dev-only `auth.provider.tested` audit action was considered and rejected: the audit log records what happened to accounts and operators' actions, a test touches neither, the console token already reaches everything in development, and an action written only in development would be an event type nothing else in the catalog could reason about. Tests are logged at INFO (`sign-in test finished` with the method, state and code, never identities or tokens), which the console's Logs screen shows.

Rate limits: sign-in's limiters (login, address, MFA, codes) are never called. The provider's return still passes the app-wide per-address limiter on `/v1/auth/*` redirects, as any request does; it counts only the developer's own address.

### 8. UI

The Authentication screen gets a **Test sign-in** tab (`?tab=tests&method=`): per method the offline checks with their fixes and links, **Check** (network checks for providers), **Test now** (popup, polling and the result), the ID token form, the passkey ceremony, the TOTP QR code and code entry, and the test email. The Routes screen links `/v1/auth/*` rows to it. Mock data in `lib/api/mock/signin-tests.ts`.

## Endpoints

| Endpoint | Answer |
|---|---|
| `GET /_dev/auth/test` | `public_url`, `methods[]`: `key`, `name`, `configured`, `checks[]`, `live` (`kind`, `available`, `reason`, `link`), `callback_url`, `id_token`, `origin`, `rp_id` |
| `POST /_dev/auth/test/{google\|apple\|github}/check` | `method`, `checks[]` (`provider_reachable`, `clock_skew`, `client_credentials`); 404 `not_configured` |
| `POST /_dev/auth/test/{google\|apple\|github\|passkeys}/start` `{"result_url"}` | 201 `id`, `url`, `expires_at`; 404 `not_configured`, 409 `live_test_unavailable`, 422 `invalid_result_url`, 429 `too_many_tests`, 400 `invalid_json` |
| `POST /_dev/auth/test/{google\|apple}/id-token` `{"id_token","nonce"}` | The result; 422 `invalid_request` |
| `GET /_dev/auth/test/results/{id}` | `id`, `method`, `kind`, `state` (pending, passed, failed, expired), `code`, `message`, `fix`, `link`, `identity`, `passkey`, `warnings`, times; 404 `test_not_found` |
| `POST /_dev/auth/test/totp/start` | 201 `id`, `secret`, `uri`, `qr_code`, `issuer`, `account`, `expires_at`, `checks` |
| `POST /_dev/auth/test/totp/verify` `{"id","code"}` | `passed`, `code` (ok, invalid_code, clock_drift, expired, too_many_attempts), `message`, `fix`, `drift_steps`, `drift_seconds`, `attempts_left` |
| `GET /_signin-test/passkey`, `POST …/options`, `POST …/finish` | The ceremony page and its steps (ticket in the body) |

Limits: 16 waiting tests of each kind, 64 results kept 30 minutes.

## Threat model

| Threat | Mitigation | Test |
|---|---|---|
| Starting a test, reading results or TOTP secrets without the token, through a tunnel, on a public `Host` | The console's Host, loopback, forwarding-header and token checks run before any extension | `TestSignInTestsRefusals`, `TestExtensions` |
| Test mode in production or without the console | No console, no `Tester`: `DevEndpoints` does nothing, the callbacks' middleware and the ceremony page aren't mounted; production refuses `DEV_CONSOLE_TOKEN` at startup | `TestSignInTestsRefusals` (console off: 404, a forged state reaches sign-in's `invalid_state`), `TestAuthSetupFromNew` |
| A forged state treated as a test | Lookup by SHA-256 of a 256-bit value in memory; anything unknown goes to sign-in | `TestGoogleRoundTrip`, `FuzzCallback` |
| A replayed test state | Marked used on first return; replays redirect to the unchanged result and never reach sign-in | `TestGoogleRoundTrip`, `TestSignInTestsCreateNothing` |
| A sign-in state used as a test's, or a test state signing someone in | Separate stores: sign-in's states are in the database and never in the tester's memory; a test's state is answered before sign-in and never written | `TestSignInTestsRefusals` (sign-in works with tests on), `TestGoogleRoundTrip` |
| A test creating accounts, links, sessions, cookies or audit events | The test path calls only `Provider` methods; no store; no cookie | `TestSignInTestsCreateNothing` compares every `auth_*` table and `audit_events` |
| Tokens or secrets in results, logs or messages | Results carry an identity summary; provider errors redacted (JWT-shaped runs, long base64, configured secrets); logs carry method, state and code | `TestRedact`, `FuzzRedact`, `TestAppleRoundTrip` (refresh token absent), `TestOverview` |
| Open redirect through `result_url` | Loopback http(s) only, no user info or fragment | `TestCheckResultURL`, `FuzzCheckResultURL` |
| The outcome posted to another origin | The page posting is on the portal's origin and posts only to `location.origin`; the screen checks `event.origin` | dashboards tests |
| The passkey page abused through a tunnel | Inert without a single-use, 5-minute ticket only the console creates; strict CSP; steps in order; each challenge answers once | `TestPasskeyCeremony` |
| Brute-forcing TOTP test codes | Five attempts per secret, which is throwaway and unrelated to any account | `TestTOTP` |
| Memory growth | 16 waiting tests of each kind, 64 results, everything expires | `TestLimits` |
| Real users' rate limits | Sign-in's limiters aren't called; only the app-wide per-address limiter counts the developer's own request | — |
| Normal sign-in changed | Interception is middleware in front of the unchanged handlers; the OpenAPI document is identical | existing `authhttp` and contract tests, `TestSignInTestsLeaveTheOpenAPIDocument` |

## Consequences

- A developer sees `redirect_uri_mismatch`, `invalid_client`, a key that doesn't belong to the team, a fast clock or an origin missing its port, with the variable to change, before any user does.
- Google's own error pages (`Error 400: redirect_uri_mismatch` shown by Google instead of returning) never reach the app; the screen shows the callback URL to register and says so while it waits.
- Apple's web round trip needs a named tunnel; the ID token check and the network check work on `localhost`.

## Known gaps

- Test state lives in one process: behind several app instances a return can land on another (not a development setup).
- The passkey test leaves a passkey in the password manager on browsers without `signalUnknownCredential`.
- The live passkey ceremony can't run on a frontend-only origin the app doesn't serve.
- No Dev Portal screenshots of the tab yet.
