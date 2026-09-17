# Secrets and keys

Every secret, key, token and code in a Full app: what it is, why it exists, who creates it, how it's stored, whether it's safe to expose, how to rotate it, and what an attacker could do with it. Verified against `modules/auth`, `modules/auth/social`, `config`, `gorbital/authhttp` and the golden apps.

For step-by-step instructions for the values you provide, see the beginner guides: [Encryption key](../sign-in/encryption-key.md), [Google](../sign-in/google.md), [Apple](../sign-in/apple.md), [Email](../sign-in/email.md), and [Every key and credential](../sign-in/all-keys.md).

## Summary

| Secret | Created by | Where it lives | Exposed if leaked? |
|---|---|---|---|
| [`AUTH_ENCRYPTION_KEYS`](#auth_encryption_keys) | You (`openssl`); `orb dev` in development | Environment | TOTP secrets, if the database leaks too |
| [`DATABASE_URL` credentials](#database_url) | Your database provider; `compose.yaml` locally | Environment | Everything |
| [`GOOGLE_CLIENT_SECRET`](#google_client_secret) | Google Cloud Console | Environment | Impersonating your app at Google's token endpoint |
| [Apple `.p8` key](#apple-private-key) | Apple Developer | File or environment | Impersonating your app at Apple, revoking users' Apple tokens |
| [`RESEND_API_KEY`](#resend_api_key-and-smtp_password) / `SMTP_PASSWORD` | Your email provider | Environment | Sending email as your domain |
| [Passwords](#passwords) | Users | `auth_users`, argon2id hash | Offline cracking of weak passwords |
| [Session tokens](#session-tokens) | The app | Client; SHA-256 in `auth_sessions` | Acting as that user until the session ends |
| [API keys](#api-keys) | The app, when a user or operator asks | The program's secret store; SHA-256 in `auth_api_keys` | Acting as the user or service account, within the key's scopes, until revoked or expired |
| [Email codes](#verification-and-reset-codes) | The app | Email; SHA-256 in `auth_codes` | Verifying an address or resetting a password, briefly |
| [TOTP secrets](#totp-secrets) | The app | Authenticator app; encrypted in `auth_totp` | Generating second-factor codes |
| [Recovery codes](#recovery-codes) | The app | User; SHA-256 in `auth_recovery_codes` | One second-factor sign-in each |
| [Passkeys](#passkeys) | The user's device | Device; public key in `auth_passkeys` | Nothing (public keys only) |
| [OAuth state, PKCE and nonces](#oauth-state-pkce-and-nonces) | The app | Database and a cookie, minutes | Nothing after use |
| [Apple client secret](#apple-client-secret) | The app, per request | Memory, 5 minutes | Short-lived impersonation at Apple |
| [Seed administrator credentials](#seed-administrator) | The seed command | Printed once | The local administrator |

**Not used, so never create them:** a JWT signing secret (sessions are opaque tokens), a session or cookie secret (cookies hold the opaque token), a CSRF secret (cross-site requests are blocked with `Sec-Fetch-Site` and `Origin` through `http.CrossOriginProtection`), a passkey server key, a webhook secret (Apple's notifications are verified with Apple's public keys), and an Apple client secret (generated per request).

## Values you provide

### `AUTH_ENCRYPTION_KEYS`

| | |
|---|---|
| **What** | One or more AES-256-GCM keys, as `id:base64` entries separated by commas: `k2:…,k1:…` |
| **Why** | TOTP secrets must be readable by the server to check codes, so they can't be hashed. Encrypting them means a database dump alone doesn't let anyone generate users' second-factor codes |
| **Format** | Each id is unique; each key decodes to exactly 32 bytes (`key "k1" must be 32 bytes in base64` otherwise). Parsed by `authlib.ParseKeyring` |
| **Generate** | `echo "k1:$(openssl rand -base64 32)"`; in PowerShell 7: `"k1:" + [Convert]::ToBase64String([System.Security.Cryptography.RandomNumberGenerator]::GetBytes(32))`. `orb dev` writes one to `.env` when empty (`cli/internal/cli/dev_keys.go`) |
| **Env** | `AUTH_ENCRYPTION_KEYS` or `AUTH_ENCRYPTION_KEYS_FILE`. Required in production and by the seed command. Empty in development: 503 `mfa_unavailable` |
| **Public-safe?** | No |
| **How it's used** | The first key encrypts; the key id is stored in a column next to each ciphertext, and any listed key decrypts. Encrypts TOTP secrets (`auth_totp`, with the user ID as AES-GCM additional data, so a ciphertext copied to another user's row doesn't decrypt) and Apple refresh tokens (`auth_identities`, and `auth_token_revocations` while they wait to be revoked, bound to provider and subject), which are needed to revoke Apple's tokens when an account is deleted |
| **Rotate** | 1. Add the new key first on every instance: `k2:…,k1:…`. 2. `cmd/api rotate-auth-keys` re-encrypts every secret with `k2`. 3. Remove `k1` |
| **If leaked** | Alone: nothing. With a database copy: an attacker can compute TOTP codes, but still needs each user's password or another first factor. Rotate the key and consider asking users to set up their authenticator again |
| **If lost** | Every TOTP secret is unreadable. Users sign in with recovery codes, or an operator runs `cmd/api reset-mfa <email>` per user. Back the key up |

### `DATABASE_URL`

| | |
|---|---|
| **What** | A PostgreSQL connection URL including a user and password |
| **Generate** | Development: `compose.yaml` sets user, password and database to the app name, bound to `127.0.0.1` only. Production: create a dedicated role with your provider; use a long random password |
| **Env** | `DATABASE_URL` or `DATABASE_URL_FILE` |
| **Public-safe?** | No. `postgres` never includes the URL in errors; `config.Secret` prints `[redacted]` in logs |
| **Rotate** | Create a second password or role, deploy with it, then revoke the old one |
| **If leaked** | Full read and write of every table, including password hashes and audit history. Rotate immediately and treat sessions as compromised: `DELETE FROM auth_sessions` signs everyone out |

### `GOOGLE_CLIENT_SECRET`

| | |
|---|---|
| **What** | The OAuth client secret of the Google **Web application** client |
| **Why** | Exchanging the authorization code for tokens at Google's token endpoint requires it, alongside PKCE (S256) |
| **Where from** | Google Cloud Console → Google Auth Platform → Clients → the web client. Shown when created |
| **Env** | `GOOGLE_CLIENT_SECRET` or `GOOGLE_CLIENT_SECRET_FILE`; required with `GOOGLE_CLIENT_ID` |
| **Public-safe?** | No. `GOOGLE_CLIENT_ID`, `GOOGLE_IOS_CLIENT_ID` and `GOOGLE_ANDROID_CLIENT_ID` are public |
| **Rotate** | In the client, **Add secret**, deploy the new one, then disable and delete the old one |
| **If leaked** | Someone could redeem authorization codes issued to your client, if they also intercept a code for a registered redirect URI; PKCE and the state cookie make that hard. Rotate |

### Apple private key

| | |
|---|---|
| **What** | A Sign in with Apple key: a `.p8` file holding an ECDSA P-256 private key in PKCS #8 PEM |
| **Why** | Apple has no static client secret: each token request carries a short JWT signed with this key |
| **Where from** | Apple Developer → Certificates, IDs & Profiles → Keys → + → Sign in with Apple. Downloadable **once** |
| **Env** | `APPLE_PRIVATE_KEY_FILE` (a path) or `APPLE_PRIVATE_KEY` (the contents). With `APPLE_KEY_ID` (its id) and `APPLE_TEAM_ID` |
| **Validation** | Parsed at start by `social.ParseApplePrivateKey`: must be PEM `PRIVATE KEY`, PKCS #8, P-256 |
| **Public-safe?** | No. The Team ID, Key ID, Services ID and bundle IDs are public |
| **Rotate** | Create a new Sign in with Apple key for the same primary App ID, deploy its `APPLE_KEY_ID` and file, confirm sign-in works, then revoke the old key |
| **If leaked** | An attacker can create valid client secrets for your app: redeem codes, and revoke your users' Apple tokens. Revoke the key at once |

### `RESEND_API_KEY` and `SMTP_PASSWORD`

| | |
|---|---|
| **What** | The credential the mail worker uses to deliver email |
| **Where from** | Resend → API Keys (permission **Sending access**, scoped to one domain); or your SMTP provider's credentials page |
| **Env** | `RESEND_API_KEY` / `RESEND_API_KEY_FILE`, or `SMTP_PASSWORD` / `SMTP_PASSWORD_FILE` with `SMTP_USERNAME`. Required when `MAIL_DELIVERY` is `provider` (always in production) |
| **Public-safe?** | No |
| **Rotate** | Create a new key, deploy it, delete the old one. Queued emails retry with the new key after the restart |
| **If leaked** | Email sent as your verified domain: phishing that passes SPF and DKIM. Delete the key at the provider |

## Values the app creates

### Passwords

Hashed with argon2id (m = 19 MiB, t = 2, p = 1, 16-byte salt, 32-byte key; [ADR-0038](../adr/0038-authentication-v0-2.md)) and stored in PHC format, `$argon2id$v=19$m=…,t=…,p=…$salt$hash`. Policy: 12 to 128 characters. Parameters are part of the stored string, so they can be raised later and old hashes still verify. Accounts created through Google or Apple have no password (`has_password: false`).

### Session tokens

| | |
|---|---|
| **Created** | `authlib.NewToken`: 32 bytes from `crypto/rand`, base64url, on sign-in |
| **Stored** | Only `SHA-256(token)` in `auth_sessions.token_hash`. A database leak doesn't reveal usable tokens |
| **Sent** | Browsers: cookie `__Host-session` (`Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`, no `Domain`). Native clients that sign in with `"transport": "bearer"`: `Authorization: Bearer <token>` |
| **Lifetime** | Idle 14 days (`auth.session_idle_ttl`), absolute 90 days (`auth.session_absolute_ttl`), both runtime settings |
| **Revoke** | `POST /v1/auth/logout`, `POST /v1/auth/logout-all`, `DELETE /v1/auth/sessions/{id}`. Resetting the password ends every session; changing it ends all but the current one; deleting the account ends all |
| **If leaked** | The holder acts as the user until the session is revoked or expires; sensitive changes still ask for the password or a passkey once the sign-in is 10 minutes old |

### API keys

| | |
|---|---|
| **Created** | `authlib.NewAPIKey`: `gbk_` + a 128-bit lookup ID + `_` + a 256-bit secret, lowercase base32, on `POST /v1/auth/api-keys` or a service account's `…/keys` ([ADR-0058](../adr/0058-api-keys-and-service-accounts.md)) |
| **Stored** | The lookup ID and `SHA-256(key)` in `auth_api_keys`; the key is shown once with `Cache-Control: no-store`. Logs and audit events carry only the lookup ID |
| **Sent** | `Authorization: Bearer gbk_…`; never accepted in a cookie, never treated as a session |
| **Lifetime** | Required expiry, at most `auth.api_key_max_ttl` (90 days by default, never over a year) |
| **Revoke** | `DELETE /v1/auth/api-keys/{id}` or the service account's keys endpoint; disabling a service account, deleting an account and resetting its password revoke every key |
| **If leaked** | The holder calls the API as the user or service account, within the key's scopes and never with permissions of roles that require two-factor authentication, until the key is revoked; it can't create keys or change the account. Secret scanners can match the `gbk_` prefix |

### Verification and reset codes

6 random digits (`authlib.NewCode`), emailed to the user. Stored as `SHA-256(code_id:code)` in `auth_codes`, single use, attempt-limited, valid 15 minutes for email verification (`auth.verification_code_ttl`) and 30 minutes for password reset (`auth.reset_code_ttl`). A 6-digit code is only safe because of the short lifetime, the attempt limit, and a limit of 20 checks a day per address across every code sent to it (`auth.code_attempts`), which a new code doesn't reset.

### TOTP secrets

Created when a user starts `POST /v1/auth/mfa/totp`, returned once as a base32 key, `otpauth://` URI and QR image, then encrypted with `AUTH_ENCRYPTION_KEYS` into `auth_totp`. Codes are RFC 6238 (SHA-1, 6 digits, 30 seconds); a used time step is recorded so a code can't be replayed on any instance.

### Recovery codes

10 codes like `hibt-qysr-vv45-ly5l`, 16 base32 characters (80 random bits), shown once when TOTP is confirmed (and by the seed command). Stored as `SHA-256(user_id:normalized code)` in `auth_recovery_codes`; each works once. 80 bits keep a leaked hash from being reversed by trying codes (codes made before 2026-09-16 have 10 characters, 50 bits; users replace them with a new set). `POST /v1/auth/mfa/recovery-codes` replaces the set.

### Passkeys

WebAuthn keeps the private key on the user's authenticator (device, password manager or security key). The app stores the credential ID, the verified credential record from `modules/auth/passkey` (public key, flags, signature counter) as JSON, the authenticator's AAGUID, backup flags and a name in `auth_passkeys`. Registration and sign-in challenges are random, stored server-side in `auth_webauthn_ceremonies`, single use and short-lived. There is **no server-side key to generate**: the relying party is identified by `WEBAUTHN_RP_ID` and `WEBAUTHN_ORIGINS`, which are public.

### OAuth state, PKCE and nonces

For web sign-in, the app creates a random state, PKCE verifier (Google) and nonce, stores them in `auth_oauth_states`, and binds the state to the browser with a short-lived `__Host-oauth` cookie. The callback must present both. For native sign-in, `POST /v1/auth/{provider}/nonce` stores a single-use nonce in `auth_social_nonces`, which the ID token must carry (hashed with SHA-256 for Apple). Each is consumed with a conditional `UPDATE … WHERE consumed_at IS NULL`, so it works once on any number of instances, and is refused after `expires_at`.

### Apple client secret

`social.appleClientSecret` signs an ES256 JWT on each call to Apple's token and revoke endpoints: header `kid` = `APPLE_KEY_ID`; claims `iss` = `APPLE_TEAM_ID`, `sub` = the Services ID or bundle ID in use, `aud` = `https://appleid.apple.com`, `iat` now and `exp` 5 minutes later. Nothing is stored.

### IDs

Public identifiers such as `usr_…`, `ses_…`, `prj_…` are a type prefix and 128 random bits in base32 (`authlib.NewID`). They aren't secrets, but they can't be guessed or enumerated.

### Seed administrator

The seed command (development only; it refuses when `APP_ENV=production`) creates `admin@example.com` with a random password, a TOTP secret and 10 recovery codes, and prints them once. It is `go run ./cmd/api seed` in an app on `gorbital.Main`, where sign-in adds it, and `go run ./cmd/seed` in a v0.1 app; `orb dev` runs it on every start. Only the argon2id hash, the encrypted TOTP secret and the recovery code hashes are stored. Lost: `POST /v1/auth/password/forgot`, `cmd/api reset-mfa`, or `docker compose down -v`.

## Storing secrets in production

- Use your platform's secret store, and prefer mounted files (`NAME_FILE`) over variables: files don't show in process listings, crash dumps of the environment or deploy logs.
- Give each environment its own values. Never copy `.env` to a server.
- Restrict who can read production secrets, and log access where the platform allows.
- `.env` is in `.gitignore`, and the repository's CI runs gitleaks. `orb add mail` refuses to save a secret to `.env` unless git ignores it.

## If a secret leaks

| Leaked | Do now |
|---|---|
| `DATABASE_URL` | Rotate the database password; `DELETE FROM auth_sessions;` to sign everyone out; review `audit_events` |
| `AUTH_ENCRYPTION_KEYS` | Rotate (add new, `rotate-auth-keys`, remove old); if the database also leaked, reset users' TOTP |
| `GOOGLE_CLIENT_SECRET` | Add a new secret, deploy, delete the old |
| Apple `.p8` | Create a new key, deploy, revoke the old key |
| `RESEND_API_KEY` / `SMTP_PASSWORD` | Delete the key at the provider; create and deploy a new one |
| A user's session token | `DELETE /v1/auth/sessions/{id}` or ask the user to sign out everywhere |
