# ADR-0059: GitHub sign-in

**Status:** Accepted (2026-09-16) · **Amends:** ADR-0024, ADR-0045, ADR-0046

## Context

v1.1 (roadmap item 4) adds GitHub login: "a non-OpenID provider in `modules/auth/social` (verified primary email from GitHub's API), web flow only, with the same linking rules". ADR-0024 listed GitHub login for v1.1 without a design. The security review also left one open item in the same files: a web sign-in without `return_to` ends on `APP_PUBLIC_URL/docs`, which answers 404 in production since docs are off there (HTTP-3). What exists:

| Area | Today | Evidence |
|---|---|---|
| Library | `social.Provider` runs Google and Apple: authorization URL with state, nonce and PKCE, code exchange, ID-token verification with go-oidc's remote key set; `socialtest` is an OIDC provider (keys, token endpoint, signed ID tokens) | `modules/auth/social/social.go`, `socialtest/server.go` |
| GitHub's protocol | OAuth 2.0 without OpenID Connect: no ID token, no JWKS, no nonce. `POST https://github.com/login/oauth/access_token` (form-encoded answer, errors in a 200 body), then `GET https://api.github.com/user` (numeric `id`, `login`, `name`) and `GET /user/emails` (`email`, `primary`, `verified`) with scopes `read:user` and `user:email`. PKCE (S256) is accepted for OAuth apps. OAuth apps have one callback URL; access tokens don't expire | GitHub docs: "Authorizing OAuth apps", REST "Users" and "Emails" |
| Account resolution | `signInWithIdentity`: a known identity signs in; a new identity with a verified email creates an account, verified only when `Identity.AuthoritativeEmail()`; an existing address links only when authoritative, otherwise `ErrSocialLinkRequired` (AUTH-M-1) | `internal/modules/auth/usecase/social.go`, ADR-0046 "Security review fixes" |
| Unverified accounts | Created by a non-authoritative provider; whoever proves the address by email runs `claimAddress`, which removes identities (unless the request is signed in to the account); `auth_cleanup` never expires an account holding an identity (`NOT EXISTS (… auth_identities …)`, provider-agnostic) | `usecase/verify_email.go`, `repository/mark_unverified_users_deleted.go`, ADR-0046 "Follow-up" |
| Explicit linking | `POST /v1/auth/identities` takes an ID token and a server nonce, after `confirmUser` and the `auth.reauth_attempts` budget. A redirect link was not added: "binding its callback to the signed-in user needs either a third-party cookie from a fetch or a second single-use token" | `LinkIdentity`, ADR-0046 |
| Web flow binding | `auth_oauth_states` row (hashed state, provider, nonce, PKCE verifier, `return_to`, 10 minutes, single use) and the `__Host-oauth` cookie holding the browser value whose hash the row stores | `StartSocialSignIn`, `FinishSocialSignIn`, `delivery/social.go` |
| Constraints | `provider IN ('google', 'apple')` on `auth_identities`, `auth_oauth_states`, `auth_social_nonces` (default names `<table>_provider_check`) | `db/migrations/20260915000006_auth_social.sql` |
| Keys | `requirePrincipal` refuses API keys (`session_required`), so keys can't manage identities | ADR-0058 |
| Default return | `DefaultReturnTo: PublicURL + "/docs"` in `app.go`; `APP_DOCS_ENABLED` defaults to false in production; the upgrade note tells builders to pass `return_to` | `internal/app/app.go`, ADR-0027 HTTP-3, `docs/guides/upgrade-notes.md` |

## Options

### Where GitHub lives in the library

| | 1. A separate type or interface for OAuth-only providers | 2. `social.NewGitHub` returning the same `*social.Provider` | 3. A generic "OAuth 2.0 with a userinfo URL" provider |
|---|---|---|---|
| App code | Every use case and the providers map learn a second type | Unchanged: `Exchange` returns the identity; `VerifyIDToken`, `ExchangeNativeCode` and `Revoke` answer `ErrNotSupported` | Unchanged, but each provider's email rules become configuration |
| API | New interface to keep stable | Additive: constructor, config, `Endpoints.APIURL`, `ErrNotSupported` | A configuration surface nobody else needs yet |
| Verdict | Rejected | **Chosen** | Rejected: GitHub's primary-and-verified email rule is specific; revisit with a third such provider |

### Which address

| | 1. The primary address, with GitHub's `verified` | 2. Any verified address | 3. The public profile `email` |
|---|---|---|---|
| Meaning | The address the person chose as theirs | Possibly an old or secondary address, picked by list order | Often empty, never marked verified |
| Verdict | **Chosen**; a primary `users.noreply.github.com` address counts as none | Rejected | Rejected |

A new person without a verified primary address is refused with the existing `social_email_unverified` (403, and `#error=` in the web flow). A returning identity signs in whatever its addresses are now: the subject is the numeric ID, not the login, which people rename.

### Linking rules

GitHub hosts nobody's mailbox, so `AuthoritativeEmail()` is always false for it (the default branch). With ADR-0046's rules unchanged, GitHub never links an existing account (`social_link_required`), and accounts it creates start unverified, are claimed by whoever proves the address by email, and are kept by `auth_cleanup` while they hold the identity. No GitHub-specific code is needed for any of this; tests prove it for GitHub.

### Linking while signed in, for a provider without ID tokens

| | 1. `GET /v1/auth/github/start?link=1` authenticated by the session cookie | 2. `POST` returns a single-use link token for the start URL | 3. `POST /v1/auth/{provider}/link` (session, password) sets `__Host-oauth` in its response and returns the provider URL; the state row records the user and session | 4. Option 3, and the callback also requires the session cookie |
|---|---|---|---|---|
| Confirming the user | No body for a password; a GET with side effects | Yes | Yes (`confirmUser`, `auth.reauth_attempts`) | Yes |
| A stranger's link or callback | Browser-bound by the cookie | Whoever holds the token (from a URL: history, logs, a shared link) links their own GitHub to the victim's account | Callback refused without the browser's cookie (`invalid_state`) | Same |
| Session ended meanwhile | Not noticed | Not noticed | Refused (`unauthenticated`): the recorded session must be active | Refused |
| Clients | Cookie sessions only | All | Frontends on the API's site (`app.example.com` with `api.example.com`) and the API's own pages; browsers blocking third-party cookies refuse the cookie for a frontend on another site | Cookie sessions only; bearer-token web apps excluded |
| Verdict | Rejected | Rejected | **Chosen** | Rejected: the active session recorded in the state already binds the flow to the account |

This is the "third-party cookie from a fetch" ADR-0046 described; it is a first-party cookie for same-site frontends, which is how the Full apps' session cookies already work, and the failure mode elsewhere is a refused link, never a wrong one. Google and Apple keep `POST /v1/auth/identities`, which needs no cookie.

### Where a sign-in without `return_to` ends

| | 1. The API's `/docs` (today) | 2. A built-in "signed in, close this window" page | 3. `AUTH_DEFAULT_RETURN_TO`, validated like `return_to`, required in production with a web provider; `/docs` in development | 4. `return_to` required in production |
|---|---|---|---|---|
| Production | 404 | Works, but a second-factor challenge or `#error=` in the fragment has nowhere to go; another HTML route under the CSP | The builder's own page, which can read the fragment | Callbacks that fail before their state is known still need somewhere to go |
| Existing production apps | Broken sign-ins without `return_to` | Nothing to do | Refuse to start until the variable is set, naming it (ADR-0045's rule for half-configured providers) | 422 for links without `return_to` |
| Verdict | Rejected | Rejected | **Chosen** | Rejected |

## Decision

### Library (`modules/auth/social`, additive)

| Addition | Behaviour |
|---|---|
| `GitHub = "github"`, `GitHubConfig{ClientID, ClientSecret, Endpoints, HTTPClient}`, `NewGitHub`, `GitHubEndpoints()`, `Endpoints.APIURL` | Authorization URL with state and PKCE S256, scopes `read:user user:email`, no nonce. `Exchange` trades the code (client secret in the body, verifier), reads `/user` and `/user/emails` with `Accept: application/vnd.github+json` and `X-GitHub-Api-Version: 2022-11-28` (10-second timeout, 1 MiB per response), and returns `Identity{Provider: "github", Subject: "<id>", Email, EmailVerified, Name (or login), Audience: client ID}`; the access token is dropped. Any refusal or non-200 is `ErrExchange` |
| `ErrNotSupported` | `VerifyIDToken` (also `ErrInvalidToken`), `ExchangeNativeCode` and `Revoke` on GitHub |
| `socialtest`: `GitHubEndpoints`, `GitHubCode(user, codeChallenge)`, `GitHubUser`, `GitHubEmail` | A fake GitHub: form-encoded token answers with errors in a 200 body like GitHub's, the S256 challenge checked against the verifier, the bearer token and Accept header required on `/user` and `/user/emails`, `EmailsStatus` to refuse the emails call |

### Apps (both Full apps)

| Area | Change |
|---|---|
| Migration `20260918000030_auth_github.sql` | `github` in the three `provider` CHECK constraints; `auth_oauth_states.link_user_id` (references `auth_users`, cascade) and `link_session_id`, both set or both NULL |
| Configuration | `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET` (secret, `_FILE`); one without the other stops the app; `APP_PUBLIC_URL` required with it in production. `AUTH_DEFAULT_RETURN_TO`: an absolute http(s) URL without user information or fragment, on `APP_PUBLIC_URL` or an `APP_CORS_ORIGINS` origin, https in production; required in production when Google, Apple web or GitHub is on; in development it defaults to `<APP_PUBLIC_URL>/docs`, and is required when `APP_DOCS_ENABLED=false` |
| Sign-in | `GET /v1/auth/github/start`, `GET /v1/auth/github/callback`: ADR-0046's web flow; audit `auth.login.succeeded` with `method: github`. `POST /v1/auth/github/nonce` and native token sign-in don't exist (the use cases answer `social_unavailable`) |
| Linking | `POST /v1/auth/{provider}/link` (`github` only) `{return_to?, password?}`: signed-in session, `confirmUser`, reauth budget; 200 `{url, expires_at}` with `Cache-Control: no-store` and the `__Host-oauth` cookie. The callback of such a state links the identity to the recorded user while the recorded session is active, signs nobody in, and redirects to `return_to`, or `#error=identity_in_use`, `unauthenticated`, `invalid_state` or `access_denied`. `auth.identity.linked` gains `flow` (`id_token` or `web`) |
| Unlinking, listing | `GET /v1/auth/identities`, `DELETE /v1/auth/identities/{id}` unchanged; `provider` enum gains `github` |
| Status | `github` in the start block, `auth-providers` and `GET /ops/auth/providers`, with the callback URL, or `GITHUB_CLIENT_ID, GITHUB_CLIENT_SECRET` and `AUTH_PROVIDERS.md#github-sign-in` |
| Unchanged | Error codes, audit action names, permissions, settings and jobs: no new public names. Rate limits cover `…/start`, `…/callback` and the link POST (under `/v1/auth/`); cross-origin protection covers the link POST; idempotency skips `/v1/auth/*` |

## Why

- One provider type keeps the app's sign-in, link and status code provider-agnostic; GitHub's differences stay in the library, where `socialtest` exercises them over HTTP.
- The numeric ID survives renames; the primary verified address is the person's own choice, and GitHub's no-reply addresses receive nothing.
- ADR-0046's authoritative-email rule already expresses what GitHub can't prove, so GitHub gets pre-account-hijacking protection without new rules.
- Binding a link to both the browser and the active session means a stolen callback, a forged link and a signed-out session all fail closed, and the password step keeps a borrowed session from adding a sign-in method.
- A configured return page removes the production 404 and gives every error and challenge fragment a page that can read it.

## Trade-offs

- Linking GitHub from a frontend on another site fails in browsers that block third-party cookies; such frontends serve the link from the API's site or a same-site subdomain.
- GitHub-created accounts stay unverified until their owner verifies by email, so they can't be given roles or accept invitations before that; a GitHub account with a Gmail address isn't trusted either.
- GitHub access tokens aren't revoked after use (they're never stored; revoking would cost a call per sign-in and doesn't remove the person's authorization).
- One OAuth app per environment, since an OAuth app has one callback URL.
- Existing production apps with Google or Apple web sign-in must set `AUTH_DEFAULT_RETURN_TO` before upgrading, or they won't start.
- GitHub Apps (as opposed to OAuth apps) aren't covered.

## Consequences

- `modules/auth` gains additive API (`api/modules-auth.txt`).
- ADR-0024: GitHub login is designed here. ADR-0045: `GITHUB_*` and `AUTH_DEFAULT_RETURN_TO` join the variables, `github` the status block. ADR-0046: the default `return_to` is no longer the API docs in production, and a redirect-based link exists for providers without ID tokens.
- Docs: [GitHub sign-in](../sign-in/github.md), `AUTH_PROVIDERS.md` "GitHub sign-in" and "After signing in", environment variables, authentication guide.

## Implementation notes (2026-09-16)

- `social.Provider` keeps no key set for GitHub; `AuthCodeURL` sends a nonce only to OpenID providers.
- `StartSocialSignIn` and `StartIdentityLink` share `startWeb`; `LinkIdentity` and the web link share `addIdentity`, whose transaction first runs the session check.
- `usecase.NewService` now requires the public URL and default return address only when a provider has a web flow: an Apple configuration for iOS apps only no longer needs them (it passed before only because the default was `"/docs"` with an empty public URL).
- `Config.returnOrigins` lower-cases origins, as the use case already did, so `AUTH_DEFAULT_RETURN_TO` is checked with the same rule as `return_to`.

| Check | Result |
|---|---|
| `TestGitHubWebSignIn`, `TestGitHubEmails`, `TestGitHubHasNoTokensOrNativeFlow` (library, fake GitHub) | Authorization URL (client, redirect, scopes, state, S256, no nonce, no form_post); token request with verifier, secret and redirect; identity from `/user` and the primary email; used code, another verifier and unknown code refused; unverified primary, no emails, a no-reply primary, emails refused (403) and a missing user ID; ID tokens, native codes, revocation and notifications refused; configuration |
| `TestGitHubSignIn` (use cases, Docker PostgreSQL) | New account unverified without a password, identity subject `583231`, audit `method: github`; renamed login signs in to the same account; no verified primary or no email refused with nothing written (`social_email_unverified`); verified and unverified accounts at `gmail.com` and `example.com` get `social_link_required` and stay unchanged; a code for another PKCE challenge, another browser and a Google state refused; no nonce or ID-token sign-in; after the unverified TTL the GitHub account is kept and a plain registration expires |
| `TestGitHubAccountIsClaimedByEmail` | The address's owner resetting the password ends the GitHub account's session, and GitHub then answers `social_link_required` |
| `TestGitHubLinkWhileSignedIn` | Wrong password (audited), Google, another site's `return_to`, signed out and an API key refused; a callback in another browser uses up the link; a link signs nobody in, emails once, audits `flow: web`, and GitHub then signs in to the owner; again changes nothing; another account's GitHub `identity_in_use`; after logout `unauthenticated`; unlinking needs the password, after which GitHub answers `social_link_required` |
| `TestGitHubSignInEndToEnd` (both apps, HTTP) | Start redirect and cookie, callback session, `/v1/auth/me` unverified without a password; `#error=social_email_unverified` and `#error=social_link_required` without a session; link: wrong password 401, signed out 401, cross-site 403, `google` 422, 200 with `no-store` and the cookie; callback in another browser `#error=invalid_state`; linked with no new session; identities; GitHub sign-in reaches the owner; `#error=identity_in_use`; `#error=unauthenticated` after logout; unlink 204; status lines on and off |
| `TestSignInReturnsToTheDefaultAddress` (both apps) | Production app (`APP_ENV=production`): a sign-in without `return_to` and a callback with an unknown state end at `AUTH_DEFAULT_RETURN_TO`, and `/docs` is 404; development: `http://localhost:8080/docs`, which is 200 |
| `TestSocialConfiguration`, `TestSignInMethodsThroughOps` | GitHub partial configuration; production without `AUTH_DEFAULT_RETURN_TO` (Google, GitHub); on the API or a CORS origin accepted; another site, http in production, fragment, user information, relative refused; development without docs; production without web sign-in and with Apple for iOS only need nothing; `github` off in `/ops/auth/providers` with what to set |
| Mutations (each reverted) | No active-session check in the link: `TestGitHubLinkWhileSignedIn` and `TestGitHubSignInEndToEnd` fail; the fake not checking PKCE: `TestGitHubWebSignIn` fails |
| `go run -C internal/tools/apicheck .` | Additions only, recorded |
| golangci-lint | Not installed locally; not run |
