# ADR-0044: Passkeys

**Status:** Accepted (2026-09-15) · **Amends:** ADR-0024, ADR-0043

## Context

ADR-0024 puts passkeys in v0.3: WebAuthn registration and sign-in, a configured relying-party ID and allowed origins, passkeys counting as a strong second factor, and `apple-app-site-association` and `assetlinks.json` for native apps. ADR-0043 built two-factor authentication with TOTP, a sign-in challenge (`POST /v1/auth/login` → 202 → `POST /v1/auth/login/mfa`), sessions verified with a second factor, and roles that require one.

Threat model row 16 (passkey phishing or origin confusion) is addressed by validating the relying-party ID and allowed origins. TOTP stays phishable; passkeys are the factor that isn't.

Three choices were made with the maintainer (2026-09-15):

| Question | Choice |
|---|---|
| What passkeys do | Passwordless sign-in **and** a second factor for password sign-ins |
| Where the relying-party ID and origins live | Environment variables: changing them is a phishing risk, so it needs a deploy and can't be done from `/ops/settings` (ADR-0031's "infrastructure" layer) |
| Native app association files | Included now, served only when configured |

## Options

### WebAuthn implementation

| | 1. `github.com/go-webauthn/webauthn` | 2. Our own verification |
|---|---|---|
| Scope | Registration and assertion ceremonies, CBOR and COSE keys, every attestation format, origin and RP ID checks, backup flags, counters | CBOR, COSE, client data, authenticator data and signature checks written and maintained by us |
| Maturity | Widely used, maintained (v0.18.1, September 2026) | New security-critical code |
| Dependencies | `fxamacker/cbor`, `go-viper/mapstructure`, `go-webauthn/x`, `golang-jwt/jwt`, `google/go-tpm`, `google/uuid`, `tinylib/msgp` | None |

Option 1, isolated behind a package so apps never import its types (threat 22).

## Decision

### Who owns what (as ADR-0038)

| Library: `gorbital.dev/modules/auth/passkey` | App: `internal/modules/auth` |
|---|---|
| `passkey.Config` (RP ID, display name, origins, Android apps) and `passkey.New`, which validates it | Registration, sign-in and second-factor flows, limits, tables, endpoints |
| `BeginRegistration` / `FinishRegistration`, `BeginLogin` (discoverable, or for one user's credentials) / `FinishLogin`, taking and returning JSON and plain structs | Stores the ceremony state and credential records as the library returns them |
| `Credential`: ID, public key record, AAGUID, backup flags, sign count; `UserHandle` generation (64 random bytes) | `auth_passkeys`, `auth_users.webauthn_user_handle` |
| `AppleAppSiteAssociation` and `AssetLinks` documents | `GET /.well-known/apple-app-site-association`, `GET /.well-known/assetlinks.json` |
| `passkeytest`: a software authenticator that creates and signs real registration and assertion responses, for tests in the library and generated apps | Tests on Docker PostgreSQL and over HTTP |

It is a separate package of the `modules/auth` module, so apps that don't import it don't compile it.

### Configuration (environment)

| Variable | Meaning | Development default |
|---|---|---|
| `WEBAUTHN_RP_ID` | The relying-party ID: the site's registrable domain, such as `example.com` | `localhost` |
| `WEBAUTHN_ORIGINS` | Comma-separated browser origins allowed to use passkeys, such as `https://app.example.com` | `http://localhost:8080,http://localhost:3000` |
| `WEBAUTHN_APPLE_APP_IDS` | Comma-separated `TEAMID.bundle.id` of iOS apps | none |
| `WEBAUTHN_ANDROID_APPS` | Comma-separated `package.name=SHA256:FINGERPRINT` entries (several fingerprints separated by `+`) | none |

- The display name is the app's `ServiceName`.
- `LoadConfig` checks that every origin is `https` (or `http://localhost` in development) and that its host is the RP ID or a subdomain of it; the library checks the rest when it starts.
- Without `WEBAUTHN_RP_ID` in production, passkeys are unavailable (503 `passkeys_unavailable`); the rest of the app runs. Passkeys are optional per deployment, unlike `AUTH_ENCRYPTION_KEYS`.
- Android apps become allowed origins (`android:apk-key-hash:<base64url of the certificate SHA-256>`), derived from `WEBAUTHN_ANDROID_APPS`.
- What developers provide, where they get each value, and how the app reports what's configured (the `.env.example` block, `AUTH_PROVIDERS.md`, the **Sign-in methods** status at start, `auth-providers` and `GET /ops/auth/providers`) follow [ADR-0045](0045-sign-in-provider-setup.md).

### Native app association files

| Path | Served when | Content |
|---|---|---|
| `GET /.well-known/apple-app-site-association` | `WEBAUTHN_APPLE_APP_IDS` is set | `{"webcredentials": {"apps": [...]}}`, `Content-Type: application/json` |
| `GET /.well-known/assetlinks.json` | `WEBAUTHN_ANDROID_APPS` is set | `delegate_permission/common.get_login_creds` statements for each package and fingerprint |

Otherwise both are 404. The files must be reachable on the RP ID's domain; when that domain is a separate web frontend, it proxies or copies them (documented).

### Tables

| Table | Columns (beyond IDs and times) |
|---|---|
| `auth_passkeys` | `user_id`, `credential_id` (unique), `credential` (the library's record as JSON: public key, flags, sign count, attestation format), `name` (the user's label), `aaguid`, `backup_eligible`, `backup_state`, `last_used_at` |
| `auth_users` | new `webauthn_user_handle` (64 random bytes, unique), set when the first passkey is registered; never the user ID, so a credential reveals nothing about the account |
| `auth_webauthn_ceremonies` | `token_hash` (unique), `user_id` (NULL for passwordless sign-in), `purpose` (`register`, `login`, `second_factor`, `reauth`), `mfa_challenge_id`, `session_data` (the library's state as JSON), `expires_at` (5 minutes), `consumed_at` |

### Registering a passkey

| Endpoint | Behaviour |
|---|---|
| `POST /v1/auth/passkeys/registration` | Signed in. Body carries the password, unless the session verified a second factor in the last 10 minutes (see *Amendment*). Returns a `ceremony_token` and the `options` for `navigator.credentials.create()`: resident key required, user verification required, attestation `none`, the user's existing passkeys excluded |
| `POST /v1/auth/passkeys` | Body: `ceremony_token`, `credential` (the browser's response) and an optional `name`. Stores the passkey (201), marks the current session verified with a second factor, and returns 10 recovery codes when the user had none yet (the first second factor creates them). Emails "passkey added" |
| `GET /v1/auth/passkeys` | The user's passkeys: ID, name, created and last used, backed up |
| `PATCH /v1/auth/passkeys/{id}` | Rename |
| `DELETE /v1/auth/passkeys/{id}` | Needs a session that verified a second factor in the last 10 minutes, or the password. Refused with 409 `mfa_required_by_role` when it's the user's last second factor and a role requires one. Emails "passkey removed" |

At most 10 passkeys per user (409 `passkey_limit_reached`).

### Signing in

**Passwordless.** `POST /v1/auth/passkeys/login/options` (public) returns a `ceremony_token` and `options` for `navigator.credentials.get()` with no allowed credentials (a discoverable sign-in) and user verification required. `POST /v1/auth/passkeys/login` takes the `ceremony_token`, `credential` and `transport`, finds the account by user handle and credential ID, and returns 200 with a session verified with a second factor, like `POST /v1/auth/login/mfa`. The account must have a verified email. Unknown credentials, wrong signatures, a missing user-verification flag, expired or used ceremonies, and deleted accounts all return 401 `invalid_passkey`.

**Second factor.** For a user with passkeys, the 202 challenge from `POST /v1/auth/login` lists `passkey` in `methods`. `POST /v1/auth/login/mfa/passkey` takes the `challenge_token` and returns a `ceremony_token` and `options` limited to that user's passkeys; `POST /v1/auth/login/mfa` then accepts `{"challenge_token", "passkey": {"ceremony_token", "credential"}, "transport"}`. Failures count as challenge attempts, as with codes (ADR-0043).

### Rules

| Topic | Decision |
|---|---|
| Two-factor authentication on | A confirmed TOTP secret **or** at least one passkey. `mfa_enabled` in `/v1/auth/me` and the role policy use this |
| Turning TOTP off | Allowed with a passkey left, even when a role requires 2FA |
| Recovery codes | Created by the first second factor, TOTP or passkey; kept while any second factor remains; deleted with the last one |
| User verification | Required for registration and passwordless sign-in; required for the second-factor ceremony too, so every passkey use proves the person, not only the device |
| Sign counter | A counter that doesn't increase (other than synced passkeys reporting 0) fails the ceremony with `invalid_passkey` and records `auth.passkey.clone_warning` |
| Backup flags | Stored and shown (`backed_up`), not enforced |
| Attestation | `none`: no metadata service, no device allowlist |
| Ceremonies | Single use, 5 minutes, bound to the purpose and, for second factors, to the sign-in challenge |
| Rate limits | The per-IP auth limit covers passkey sign-in; second-factor attempts count as in ADR-0043 |
| Operator reset | `reset-mfa` also removes the user's passkeys |

### Audit actions and emails

`auth.passkey.registered`, `auth.passkey.renamed`, `auth.passkey.removed`, `auth.login.succeeded` (metadata `method: passkey`), `auth.login.failed` (reason `invalid_passkey`), `auth.mfa.challenge_succeeded` (method `passkey`), `auth.passkey.clone_warning`. Credential IDs are recorded as the passkey's row ID, never the raw credential. Emails: passkey added, passkey removed.

### Error codes

`invalid_passkey` (401), `passkey_not_found` (404), `passkey_limit_reached` (409), `passkeys_unavailable` (503); `mfa_required_by_role` (409) and `invalid_mfa` (401) as in ADR-0043.

## Why

- Passkeys resist phishing: the browser binds each signature to the origin, and the server checks the RP ID and origins it was configured with.
- Environment configuration keeps the phishing-relevant part of passkeys out of reach of a compromised ops account (threat 23).
- A maintained library for WebAuthn's parsing and verification is safer than new cryptographic code; wrapping it keeps apps free of its types and its churn.
- Treating a passkey sign-in as verified lets ops users drop TOTP entirely.
- A software authenticator in tests exercises the real verification path instead of mocks.

## Trade-offs

- Seven more modules in `modules/auth`'s graph, compiled only into apps that import `passkey`.
- Browsers need HTTPS or `localhost`, and the RP ID can't be an IP address: `http://127.0.0.1:8080` can't use passkeys in development; use `http://localhost:8080`.
- Rejecting a counter that goes backwards can lock out an authenticator with a buggy counter; the user signs in another way and removes it.
- Association files on a domain the API doesn't serve need the frontend's help.
- No seed passkey: registering one needs a browser or device.

## Consequences

- `modules/auth/passkey` and `passkeytest` (public API, ADR-0015); `modules/auth` gains `SendPasskeyAdded` and `SendPasskeyRemoved` emails.
- The Full preset gains a migration, repository files, use cases, endpoints, the `.well-known` routes, `WEBAUTHN_*` in `.env.example`, and tests for registration, both sign-in paths, exclusions, limits, counters, the last-factor rule and the association files.
- ADR-0043's "two-factor authentication on" becomes TOTP or passkeys; `reset-mfa` removes passkeys too.
- Threat model row 16 is addressed when this ships.

## Amendment: confirming the user (2026-09-15)

A principal-engineer review before merge found that a session stayed "verified with a second factor" for its whole life. A stolen session verified weeks earlier could register the attacker's passkey without the password, and the passkey outlived password reset and logout-all. Accounts with only passkeys also had no way to give a second factor outside sign-in, so deleting the account needed a recovery code.

| Topic | Decision |
|---|---|
| Recent verification | `auth.Principal` carries `MFAVerifiedAt`; `RecentlyVerified` is true for 10 minutes (`auth.RecentVerification`). Verifying a second factor (sign-in, confirming the authenticator app, adding a passkey) sets the time again |
| Adding or removing a passkey | No password only within those 10 minutes. Otherwise the password, and with two-factor authentication on, a session that verified a second factor (401 `invalid_mfa` otherwise) |
| First second factor | Adding the first passkey signs out other devices, as confirming the authenticator app does |
| Confirming with a passkey | `POST /v1/auth/passkeys/verification` (signed in) returns options limited to the user's passkeys; the ceremony (purpose `reauth`) is bound to the user and to no sign-in challenge. `DELETE /v1/auth/me`, `DELETE /v1/auth/mfa/totp` and `POST /v1/auth/mfa/recovery-codes` accept its response as `passkey`. A sign-in's second-factor ceremony doesn't count, and a verification ceremony doesn't finish a sign-in |
| Replacing recovery codes | A code or a passkey response; never a recovery code, and no longer "any session verified with a passkey" |
| Signature counter | The passkey row is locked (`FOR UPDATE`) during a sign-in, so simultaneous sign-ins update it one after the other |
| Ceremony cleanup | `auth_cleanup` deletes ceremonies once expired, since public endpoints create them |

Rate limits stay per instance (ADR-0043); limits shared across instances are a v1.0 hardening item.

## Implementation notes (2026-09-15)

- `modules/auth/passkey` adds seven modules to `modules/auth`'s graph (`fxamacker/cbor`, `go-viper/mapstructure`, `go-webauthn/x`, `golang-jwt/jwt`, `google/go-tpm`, `google/uuid`, `tinylib/msgp`) and `rsc.io/qr` for authenticator app QR codes; all tests and lint pass on Go 1.26.
- The library's credential record and ceremony state are stored as `jsonb` and passed back unchanged; the app never parses them.
- Two-factor authentication is on with a confirmed authenticator app or at least one passkey: sign-in challenges list only the methods this server can check (`totp` needs `AUTH_ENCRYPTION_KEYS`, `passkey` needs `WEBAUTHN_RP_ID`), plus `recovery_code`.
- Turning the authenticator app off keeps the recovery codes while a passkey remains; removing the last second factor deletes them.
- Replacing recovery codes, account deletion and turning the authenticator app off take a passkey response too (see *Amendment*).
- `auth_cleanup` removes ceremonies once they expire.
- A failed passkey sign-in records `auth.login.failed` with reason `invalid_passkey`; a successful one records `auth.login.succeeded` with `mfa_method: passkey`.

| Check | Result |
|---|---|
| Library (`passkeytest` software authenticator) | Registration and both sign-in paths; exclusions; wrong origin, missing user verification, garbage and other-challenge responses refused; unknown credentials; lookup errors passed through; clone warning with a repeated counter; synced passkeys with counter 0; config validation; Apple and Android parsing, association files and the Android origin |
| Use cases (Docker PostgreSQL) | Registration with and without the password, recovery codes for the first second factor, single-use ceremonies, passwordless sign-in, passkey second factor bound to its challenge, list, rename, remove, wrong origin, missing user verification, clone warning audited, unknown passkey, the 10-passkey limit, no configuration, last-factor rule with roles, authenticator app turned off with a passkey left, operator reset |
| HTTP | Add, passwordless sign-in, used ceremony refused, second factor, list, rename, remove; both `.well-known` files served when configured and 404 otherwise; `WEBAUTHN_*` validation |
