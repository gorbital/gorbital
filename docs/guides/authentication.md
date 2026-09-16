# Authentication guide

How a Full preset app signs people up and in, keeps them signed in, and decides what they may do. Implemented in `examples/full-single` (`internal/modules/auth`) on the building blocks of `modules/auth`. Decisions: [ADR-0024](../adr/0024-authentication-methods.md), [ADR-0038](../adr/0038-authentication-v0-2.md), [ADR-0043](../adr/0043-two-factor-authentication.md) (two-factor authentication).

## The flow

```text
register ──► email with a 6-digit code ──► verify-email ──► login ──► session (cookie or token)
                                                              │
                                  2FA on? ──► 202 challenge ──► login/mfa (code or recovery code)
                                                                         │
                                         /v1/auth/me, /ops/* (with a role and 2FA), your endpoints
```

Start the app with `orb dev` (or `docker compose up -d --wait`, `go run ./cmd/migrate`, `go run ./cmd/api`). Emails land in Mailpit at http://127.0.0.1:8025.

```bash
# 1. Create an account
curl -X POST http://127.0.0.1:8080/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"a long enough password"}'

# 2. Read the code in Mailpit, then verify the address
curl -X POST http://127.0.0.1:8080/v1/auth/verify-email \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","code":"123456"}'

# 3. Sign in and keep the token (native clients, scripts)
TOKEN=$(curl -s -X POST http://127.0.0.1:8080/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"a long enough password","transport":"bearer"}' | jq -r .token)

curl http://127.0.0.1:8080/v1/auth/me -H "Authorization: Bearer $TOKEN"
```

## Where the code lives

Your app owns authentication like any other module, with all four layers. The gorbital library only supplies the security building blocks.

| Folder | What's in it |
|---|---|
| `internal/modules/auth/domain/` | `User`, `Session`, `Code`, `TOTP`, `MFAChallenge` and the module's errors |
| `internal/modules/auth/usecase/` | The flows: `register.go`, `verify_email.go`, `login.go`, `login_mfa.go`, `sessions.go` (authenticate, sessions, logout), `password.go` (reset and change), `mfa.go` (set up, turn off, recovery codes), `mfa_admin.go` (operator enrollment, reset, key rotation), `account.go` (delete, create user, roles, cleanup); `ports.go` lists what they need from storage |
| `internal/modules/auth/repository/` | `store.go` with transactions, and one SQL file per operation: `insert_user.go`, `select_user.go`, `insert_session.go`, `use_totp_step.go`, `use_recovery_code.go`, … |
| `internal/modules/auth/delivery/` | The `/v1/auth` endpoints (`auth.go`, `mfa.go`) |
| `internal/app/permissions.go` | Permissions, platform roles, and which roles require two-factor authentication |
| `internal/app/keys.go` | `AUTH_ENCRYPTION_KEYS`, which encrypts authenticator app secrets |
| `internal/app/admin.go`, `admin_mfa.go` | The `grant-role`, `revoke-role`, `roles`, `reset-mfa` and `rotate-auth-keys` commands |
| `db/migrations/…_auth.sql`, `…_auth_mfa.sql` | The `auth_users`, `auth_sessions`, `auth_codes`, `auth_user_roles`, `auth_totp`, `auth_recovery_codes` and `auth_mfa_challenges` tables |
| `gorbital.dev/modules/auth` (library) | Password hashing, tokens, codes, TOTP, the encryption keyring, recovery codes, cookies, the request middleware, the permission catalog, plain emails |

Change a rule, such as allowing only your company's email domain, in the use case (`register.go`); add a column with a new migration and a repository file.

## Your first administrator

`/ops/*` needs a platform role, and a session signed in with two-factor authentication. In development, seed data already created one: the first `orb dev` (or `go run ./cmd/seed`) creates `admin@example.com` with `platform_admin` and two-factor authentication on, and prints its random password, authenticator app key and recovery codes once, without saving them ([ADR-0042](../adr/0042-development-seed-data.md)). Add the key to an authenticator app and sign in as in [Two-factor authentication](#two-factor-authentication).

To give your own account a role, in development or production, register and verify it as above, then grant the role from the app's directory:

```bash
go run ./cmd/api roles                                        # list roles and their permissions
go run ./cmd/api grant-role you@example.com platform_admin    # recorded in the audit log as "cli"
```

The role applies to your next request. `platform_admin` and `ops_viewer` require two-factor authentication: until the account turns it on and the session is verified with a second factor, `/ops/*` answers 403 `mfa_required`. `go run ./cmd/api revoke-role <email> <role>` takes a role away.

| Role | Can | Requires 2FA |
|---|---|---|
| `platform_admin` | Everything under `/ops`: settings, jobs, audit log, email | Yes |
| `ops_viewer` | Read settings, jobs, the audit log and email status; change nothing | Yes |

Add roles and permissions in `internal/app/permissions.go`; `c.RequireMFA("role")` makes a role require two-factor authentication. It's code, not a runtime setting, so nobody can switch it off from `/ops/settings`.

## Two-factor authentication

Any account can turn on two-factor authentication with an authenticator app (TOTP: Google Authenticator, 1Password, Authy and others). Roles that require it can't be used without it.

```bash
# 1. Start: send the password, get a secret and an otpauth:// URI (show it as a QR code)
curl -X POST http://127.0.0.1:8080/v1/auth/mfa/totp -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"password":"a long enough password"}'

# 2. Confirm: send a code from the app, get 10 recovery codes (shown once)
curl -X POST http://127.0.0.1:8080/v1/auth/mfa/totp/confirm -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"code":"123456"}'

# Later sign-ins take two steps: the password returns 202 and a challenge...
CHALLENGE=$(curl -s -X POST http://127.0.0.1:8080/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"a long enough password"}' | jq -r .mfa.challenge_token)
# ...and a code (or "recovery_code") finishes it
TOKEN=$(curl -s -X POST http://127.0.0.1:8080/v1/auth/login/mfa -H 'Content-Type: application/json' \
  -d "{\"challenge_token\":\"$CHALLENGE\",\"code\":\"123456\",\"transport\":\"bearer\"}" | jq -r .token)
```

- Confirming turns it on, verifies the current session with a second factor and signs out other devices.
- Each code works once: a code already used, even on another server instance, is refused. Wait for the app's next code.
- A challenge lasts 5 minutes and allows 5 attempts; the attempts count toward the login limit.
- A recovery code works once; the user gets an email saying how many are left. `POST /v1/auth/mfa/recovery-codes` with a code or a passkey response replaces them all (a recovery code can't).
- Turning it off (`DELETE /v1/auth/mfa/totp`) needs the password and a code, recovery code or passkey response, and isn't allowed while a role requires it. Deleting the account also needs one of them.
- Password reset doesn't turn it off: the next sign-in still asks for a code.
- Lost the app and the recovery codes: an operator runs `go run ./cmd/api reset-mfa <email>`, which turns it off and signs the account out everywhere.

### Encryption keys

Authenticator app secrets are stored encrypted (AES-256-GCM) with `AUTH_ENCRYPTION_KEYS`: comma-separated `id:base64key` entries of 32-byte keys, the first encrypting and all decrypting.

| Environment | What happens |
|---|---|
| Production | Required: the app doesn't start without a valid key. Generate one with `echo "k1:$(openssl rand -base64 32)"` and keep it in your secret store |
| Development | `orb dev` writes a random key to `.env` when it's empty. Without a key (plain `go run`), two-factor endpoints answer 503 `mfa_unavailable`, and accounts with it on can't sign in |

To replace a key: put the new key first and keep the old one (`k2:…,k1:…`) on every instance, run `go run ./cmd/api rotate-auth-keys` to re-encrypt every secret, then remove the old key. Losing every key turns off everyone's second factor until operators reset it, so back keys up like database credentials.

## Passkeys

Passkeys sign people in with Face ID, Touch ID, Windows Hello, an Android phone or a security key ([ADR-0044](../adr/0044-passkeys.md)): with no password at all, or as the second factor after one. A passkey sign-in counts as two-factor authentication, so ops roles work with passkeys alone. What to configure, and for native apps where to find each value: [sign-in provider setup](auth-providers.md).

```text
Add:        POST /v1/auth/passkeys/registration → navigator.credentials.create(options) → POST /v1/auth/passkeys
Sign in:    POST /v1/auth/passkeys/login/options → navigator.credentials.get(options) → POST /v1/auth/passkeys/login
2nd factor: POST /v1/auth/login (202, methods include passkey) → POST /v1/auth/login/mfa/passkey → get() → POST /v1/auth/login/mfa {"passkey": …}
Confirm:    POST /v1/auth/passkeys/verification → get() → DELETE /v1/auth/me, DELETE /v1/auth/mfa/totp or POST /v1/auth/mfa/recovery-codes {"passkey": …}
```

| Endpoint | Needs a session | Purpose | Success |
|---|---|---|---|
| `POST /v1/auth/passkeys/registration` | ✓ | `{password}` unless the session verified a second factor in the last 10 minutes; returns `ceremony_token` and `options` | 200 |
| `POST /v1/auth/passkeys` | ✓ | `{ceremony_token, name?, credential}`; recovery codes when it's the first second factor | 201 `{passkey, recovery_codes?}` |
| `GET /v1/auth/passkeys` | ✓ | The user's passkeys | 200 |
| `PATCH /v1/auth/passkeys/{id}` | ✓ | `{name}` | 204 |
| `DELETE /v1/auth/passkeys/{id}` | ✓ | `{password}` unless the session verified a second factor in the last 10 minutes | 204 |
| `POST /v1/auth/passkeys/verification` | ✓ | Options limited to the user's passkeys, to confirm a sensitive change | 200 |
| `POST /v1/auth/passkeys/login/options` | | Start a passwordless sign-in | 200 |
| `POST /v1/auth/passkeys/login` | | `{ceremony_token, credential, transport?}` | 200 `{user, session, token?}` |
| `POST /v1/auth/login/mfa/passkey` | | `{challenge_token}`; options limited to the account's passkeys | 200 |

- `credential` is the browser's `PublicKeyCredential` as JSON (`credential.toJSON()`); `options` go to `PublicKeyCredential.parseCreationOptionsFromJSON` or `parseRequestOptionsFromJSON`, or a WebAuthn helper library.
- Each ceremony works once and lasts 5 minutes. Up to 10 passkeys per account.
- The device must verify the user (biometrics or PIN). A passkey whose signature counter goes backwards is refused and recorded as `auth.passkey.clone_warning`.
- Adding or removing a passkey needs the password once the session's second factor is 10 minutes old, so a stolen session can't plant its own passkey. With two-factor authentication on, the session must also have verified one.
- The first passkey signs out other devices, like turning on the authenticator app.
- The last second factor can't be removed while a role requires one; `reset-mfa` removes passkeys too.
- In development, open the app at `http://localhost:8080`: browsers don't allow passkeys on `127.0.0.1`.

Errors: `invalid_passkey` (401), `passkey_not_found` (404), `passkey_limit_reached` (409), `invalid_passkey_name` (422), `passkeys_unavailable` (503, `WEBAUTHN_RP_ID` not set).

## Google and Apple sign-in

People sign in with their Google or Apple account, in browsers and in native apps ([ADR-0046](../adr/0046-google-and-apple-sign-in.md)). Each is off until its credentials are set; creating them step by step: [sign-in provider setup](auth-providers.md) and the app's `AUTH_PROVIDERS.md`.

```text
Browser:    GET /v1/auth/{google,apple}/start?return_to=… → provider → callback → 303 to return_to (session cookie, #mfa_challenge_token=…, or #error=…)
Native app: POST /v1/auth/{provider}/nonce → SDK sign-in with the nonce → POST /v1/auth/{provider}/token {id_token, nonce}
```

| Endpoint | Needs a session | Purpose | Success |
|---|---|---|---|
| `GET /v1/auth/{provider}/start` | | `?return_to=` an absolute URL on the API's origin or `APP_CORS_ORIGINS`; sets a 10-minute `__Host-oauth` cookie and redirects | 302 |
| `GET /v1/auth/google/callback`, `POST /v1/auth/apple/callback` | | The provider returns here; redirects to `return_to` | 303 |
| `POST /v1/auth/{provider}/nonce` | | A single-use nonce for 5 minutes (Apple's iOS SDK takes its SHA-256 in hex) | 200 `{nonce, expires_at}` |
| `POST /v1/auth/google/token` | | `{id_token, nonce, transport?}` from iOS or Android | 200 session or 202 challenge |
| `POST /v1/auth/apple/token` | | `{id_token, nonce, authorization_code?, name?, transport?}` from iOS | 200 session or 202 challenge |
| `GET /v1/auth/identities` | ✓ | Linked Google and Apple accounts | 200 `{identities}` |
| `POST /v1/auth/identities` | ✓ | `{provider, id_token, nonce, authorization_code?, name?, password}`: link the provider account of an ID token (nonce from `POST /v1/auth/{provider}/nonce`); `password` unless the second factor is under 10 minutes old | 201 `{identity}`, 200 when already linked |
| `DELETE /v1/auth/identities/{id}` | ✓ | `{password}` unless the second factor is under 10 minutes old; accounts without a password sign in again first | 204 |
| `POST /v1/auth/apple/notifications` | | Apple's server-to-server notifications | 204 |

- **New people** get an account with no password (`user.has_password` false); they can set one with "forgot password". The address counts as verified (`user.email_verified`) only when the provider manages it (below). Otherwise the account works for signing in with that provider, but whoever proves the address by email later (a verification code or password reset) takes the account over: its sessions end and the provider link is removed. The person themselves verifies while signed in (`POST /v1/auth/verify-email/resend`, then `POST /v1/auth/verify-email` with their session) and keeps the link.
- **An existing account with the same email** is linked by signing in only when the provider manages the address: Google for `gmail.com`, `googlemail.com` and the account's own Google Workspace domain (the `hd` claim), Apple for iCloud (`icloud.com`, `me.com`, `mac.com`) and its private relay addresses. The owner gets an email. For any other address a provider's "verified" only means the person controlled it when they added it, maybe years ago, so sign-in answers `social_link_required` (403): the owner signs in and links the provider with `POST /v1/auth/identities`, sending an ID token from Google's or Apple's SDK in a native app, or from Google Identity Services or Sign in with Apple JS in a browser.
- If that account never verified its email, linking by sign-in removes its password, ends its sessions, and removes any passkey, authenticator app or other identity, so whoever registered the address without owning it loses access.
- **Two-factor authentication** still applies: an account with it on gets a challenge, as with a password.
- **Accounts without a password** confirm sensitive changes (authenticator app setup, deleting the account, passkeys, unlinking) with a sign-in less than 10 minutes old.
- The last way to sign in can't be unlinked. Deleting the account (or unlinking Apple) queues Apple's tokens for the `auth_revoke_tokens` job, which revokes them within a minute and retries if Apple is unavailable; Apple's "consent revoked" notification unlinks Apple too.
- Apple's notifications are accepted for an hour after Apple signs them, each once; one about an event from before Apple was linked leaves the new link alone. A leaked notification can't be replayed later.

Errors: `invalid_social_token` (401), `invalid_state` (401), `social_email_unverified` (403), `social_link_required` (403), `identity_not_found` (404), `last_sign_in_method` (409), `identity_in_use` (409, linking a provider account another account has), `invalid_return_to` (422), `social_unavailable` (503). The web flow puts the same codes in `#error=`, plus `access_denied` when the person cancels.

## Browsers and native apps

| Client | Login body | Result | Later requests |
|---|---|---|---|
| Browser (default) | `{"email", "password"}` | `Set-Cookie: __Host-session=…; Secure; HttpOnly; SameSite=Lax` | The browser sends the cookie; scripts can't read it |
| Mobile, desktop, CLI | `{"email", "password", "transport": "bearer"}` | `{"token": "…"}` in the body, shown once | `Authorization: Bearer <token>` |

With two-factor authentication on, send `transport` to `POST /v1/auth/login/mfa` instead: that step creates the session.

- Browsers need HTTPS in production (the cookie is `Secure`); `localhost` works in development.
- Cross-site requests carrying the cookie are refused (403), so other websites can't act as your users. Set `APP_CORS_ORIGINS` for your own frontend's origin.
- Store native tokens in the platform's secure storage (Keychain, Keystore).

## Endpoints

| Method and path | Needs a session | Purpose | Success |
|---|---|---|---|
| `POST /v1/auth/register` | | Create an account; emails a code | 202 |
| `POST /v1/auth/verify-email` | | `{email, code}` | 204 |
| `POST /v1/auth/verify-email/resend` | | `{email}`; at most once a minute | 202 |
| `POST /v1/auth/login` | | `{email, password, transport?}` | 200 `{user, session, token?}`, or 202 `{mfa: {challenge_token, methods, expires_at}}` with 2FA on |
| `POST /v1/auth/login/mfa` | | `{challenge_token, code, recovery_code or passkey, transport?}` | 200 `{user, session, token?}` |
| `POST /v1/auth/password/forgot` | | `{email}`; emails a reset code | 202 |
| `POST /v1/auth/password/reset` | | `{email, code, password}`; signs out every device | 204 |
| `GET /v1/auth/me` | ✓ | User, current session, permissions, `step_up_permissions`, `mfa_enabled`, `mfa_required` | 200 |
| `PUT /v1/auth/password` | ✓ | `{current_password, new_password}`; signs out other devices | 204 |
| `GET /v1/auth/sessions` | ✓ | Signed-in devices, `current` marks this one, `mfa_verified` | 200 |
| `DELETE /v1/auth/sessions/{id}` | ✓ | Sign out one device | 204 |
| `POST /v1/auth/logout` | ✓ | Sign out this device | 204 |
| `POST /v1/auth/logout-all` | ✓ | Sign out every device | 200 `{revoked}` |
| `DELETE /v1/auth/me` | ✓ | `{password, code, recovery_code or passkey with 2FA}`; delete the account | 204 |
| `POST /v1/auth/mfa/totp` | ✓ | `{password}`; start setting up an authenticator app | 200 `{secret, uri, qr_code}` |
| `POST /v1/auth/mfa/totp/confirm` | ✓ | `{code}`; turn two-factor authentication on | 200 `{recovery_codes}` |
| `DELETE /v1/auth/mfa/totp` | ✓ | `{password, code, recovery_code or passkey}`; turn it off | 204 |
| `POST /v1/auth/mfa/recovery-codes` | ✓ | `{code or passkey}`; replace the recovery codes | 200 `{recovery_codes}` |

## What users see

- **Registration** always answers "check your email", even if the address already has an account, and takes the same time (at least 300 ms) either way; the owner of an existing account gets an email saying someone tried to sign up. Resending a code and forgot password take the same time too.
- **Registering again before verifying** sends a new code (at most once a minute) and keeps the password only when it's the same one. With a different password the account is left without one: nobody has proven they own the address yet, so neither the first nor the last registrant gets to choose the password of the account the owner verifies. After verifying, the owner sets a password with forgot password. Show that option when sign-in answers `invalid_credentials` right after verification.
- **Verifying an address**, with its code or with a password reset code, signs out every session and removes any passkey, authenticator app, recovery codes and Google or Apple link the account got before its address was proven, unless the request is signed in to that account.
- **Unverified accounts expire**: the `auth_cleanup` job deletes accounts still unverified after `auth.unverified_account_ttl` (7 days), freeing the address. Accounts with a Google or Apple link, and accounts sent a code within that time, are kept.
- **Roles** go only to verified accounts: `grant-role` refuses an unverified one.
- **Wrong email or password** is one answer: `invalid_credentials`. `email_not_verified` appears only after the right password.
- **Forgot password** always answers "check your email".
- **Codes** are 6 digits, expire (15 minutes to verify, 30 to reset, adjustable), allow 5 tries, and a new one replaces the old one. An address gets 20 code checks a day across all its codes (`auth.code_attempts`, `auth.code_window`), whether or not it has an account; past that even the right code gets `too_many_attempts` until the window passes, so a new code every minute doesn't bring new guesses.
- **Email addresses** with an uppercase non-ASCII letter, or a character that lowercases to another one such as the Kelvin sign (K), are refused as `invalid_email`: they would share the account of a look-alike address. Lowercase non-ASCII addresses such as `jürgen@bücher.example` work.
- **Passwords** need 12 to 128 characters; `weak_password` says what's wrong.
- **Too many attempts**, answered with `too_many_attempts` saying how long to wait:

  | Limit | Default | Setting |
  |---|---|---|
  | Sign-ins per address from one network (an IPv4 address or IPv6 /64), second factors included | 10 per 15 minutes | `auth.login_attempts`, `auth.login_window` |
  | Sign-ins per address from all networks | 50 per 15 minutes | `auth.login_address_attempts` |
  | Auth requests per client network | 60 per minute | `auth.ip_requests_per_minute` |
  | Code checks per address | 20 per day | `auth.code_attempts`, `auth.code_window` |
  | Changes to two-factor authentication per user | 10 per 15 minutes | `auth.mfa_change_attempts` |
  | Changes that check the password or a second factor of a signed-in user (password change, authenticator app setup and turning it off, adding or removing a passkey, linking or unlinking Google or Apple, account deletion) | 10 per 15 minutes | `auth.reauth_attempts` |

  Wrong passwords from someone else's network don't lock the owner out; reaching the address-wide limit does, for the rest of the window. A stolen session can't guess the password through the changes that check it. The limits are shared by every instance (PostgreSQL, [ADR-0052](../adr/0052-shared-rate-limits.md)); if the database doesn't answer, each instance applies them on its own until it does. Behind a load balancer, set `APP_TRUSTED_PROXIES` so the per-network limits apply to each client rather than to the balancer.
- **Two-factor authentication** emails the user when it's turned on or off and when a recovery code is used.

## Sessions

- A session ends after 14 days without use or 90 days in total (runtime settings `auth.session_idle_ttl`, `auth.session_absolute_ttl`), or when signed out.
- A session is `mfa_verified` when it was created with a second factor, or when the user confirmed two-factor authentication in it. Roles requiring 2FA grant their permissions only to such sessions.
- Changing the password signs out other devices; resetting it or deleting the account signs out every device; turning two-factor authentication on or off signs out other devices.
- The `auth_cleanup` job (daily, 03:30 UTC) removes ended sessions after 7 days, old codes and sign-in challenges, and deleted accounts after `auth.deleted_account_retention` (30 days). It deletes accounts still unverified after `auth.unverified_account_ttl` (7 days), recording `auth.accounts.unverified_expired` with the count.
- The `ratelimit_cleanup` job (hourly) deletes rate limit buckets that are full again.
- The `auth_revoke_tokens` job (every minute) revokes queued Apple tokens, retrying after 1, 2, 4 … minutes up to 6 hours; after 10 failures it gives up and records `auth.identity.revocation_abandoned`.

## Settings

| Key | Default | Range | Reason required |
|---|---|---|---|
| `auth.session_idle_ttl` | 14 days | 1 hour – 90 days | Yes |
| `auth.session_absolute_ttl` | 90 days | 1 – 365 days | Yes |
| `auth.verification_code_ttl` | 15 minutes | 5 minutes – 1 hour | Yes |
| `auth.reset_code_ttl` | 30 minutes | 10 minutes – 2 hours | Yes |
| `auth.deleted_account_retention` | 30 days | 1 – 365 days | Yes |
| `auth.unverified_account_ttl` | 7 days | 1 hour – 90 days | Yes |

Rate limit settings (group `rate_limits`, all with a reason required) are in the table under [What users see](#what-users-see): `auth.ip_requests_per_minute` (10 – 10 000), `auth.login_attempts` (3 – 100), `auth.login_address_attempts` (10 – 1000), `auth.login_window` (1 minute – 24 hours), `auth.mfa_change_attempts` (3 – 100), `auth.reauth_attempts` (3 – 100), `auth.code_attempts` (5 – 100) and `auth.code_window` (1 hour – 7 days).

Change them with `PUT /ops/settings/{key}`. The auth module also enforces hard limits of its own, so no setting can make sessions or codes unsafe. Two-factor authentication has no runtime settings.

## In your own code

Use cases get the signed-in user from the context; the middleware sets it for every request.

```go
import authlib "gorbital.dev/modules/auth"

func (s *Service) CreateProject(ctx context.Context, name string) (Project, error) {
	switch err := actor.Require(ctx, "projects.create"); {
	case errors.Is(err, actor.ErrUnauthenticated):
		return Project{}, ErrUnauthenticated
	case errors.Is(err, actor.ErrStepUpRequired):
		return Project{}, ErrMFARequired // the role requires 2FA and this session hasn't used it
	case err != nil:
		return Project{}, ErrForbidden
	}
	p, _ := authlib.PrincipalFrom(ctx) // user, session and permissions, when you need more than the actor
	// …
}
```

1. Declare the permission in `internal/app/permissions.go` and add it to a role.
2. Check it in the use case with `actor.Require` (or `actor.Can` when you only need yes or no).
3. Map your module's errors to `unauthenticated` (401), `mfa_required` (403) and `forbidden` (403) in `module_<name>.go`.

## Error codes

| Code | Status | When |
|---|---|---|
| `unauthenticated` | 401 | No valid session |
| `forbidden` | 403 | Signed in without the permission |
| `mfa_required` | 403 | The permission's role requires two-factor authentication, and this session wasn't verified with a second factor |
| `invalid_credentials` | 401 | Wrong email or password |
| `email_not_verified` | 403 | Right password, address not verified yet |
| `invalid_email` | 422 | Not an email address |
| `weak_password` | 422 | Password fails the policy; `detail` says why |
| `invalid_code` | 422 | Wrong, used or expired email code |
| `invalid_mfa` | 401 | Wrong or already used second factor, or an expired, used or exhausted sign-in challenge |
| `mfa_already_enabled` | 409 | Setting up two-factor authentication when it's already on |
| `mfa_not_enabled` | 409 | Confirming without starting, or turning off or replacing codes when it's off |
| `mfa_required_by_role` | 409 | Turning two-factor authentication off while a role requires it |
| `mfa_unavailable` | 503 | `AUTH_ENCRYPTION_KEYS` isn't set on this server |
| `too_many_attempts` | 429 | Rate limited; `detail` says how long to wait |
| `session_not_found` | 404 | Revoking a session that isn't yours or has ended |
| `social_link_required` | 403 | Signing in with Google or Apple for the address of an existing account that provider doesn't manage; sign in and link it with `POST /v1/auth/identities` |
| `identity_in_use` | 409 | Linking a Google or Apple account that another account has |
| `auth_unavailable` | 503 | The session store couldn't be reached, or too many passwords are being checked at once (each waits up to 5 seconds for its turn) |

## Audit events

Every sign-in (successful or not), second factor (`auth.mfa.challenge_succeeded`, `auth.mfa.challenge_failed`, `auth.mfa.recovery_code_used`), verification, password change or reset, two-factor change (`auth.mfa.totp_enabled`, `auth.mfa.totp_disabled`, `auth.mfa.recovery_codes_regenerated`, `auth.mfa.reset`, `auth.keys.rotated`), sign-out, account deletion and role change is recorded, as is every wrong password or second factor given behind a session (`auth.reauth.failed`, `reason` `invalid_credentials`, `invalid_mfa` or `rate_limited`), with the client's IP address and user agent. See them with `GET /ops/audit?action_prefix=auth.`. Email addresses, secrets, codes and recovery codes are never stored in event metadata. Every action with its metadata keys: [audit actions reference](../reference/audit-actions.md); roles and permissions: [permissions reference](../reference/permissions.md).

## Troubleshooting

| Symptom | Fix |
|---|---|
| No code arrives | Check Mailpit (http://127.0.0.1:8025) in development, or `GET /ops/jobs/runs?kind=gorbital.mail.send` for delivery errors ([email guide](email.md)) |
| `email_not_verified` | Verify with the emailed code, or `POST /v1/auth/verify-email/resend` |
| `forbidden` on `/ops/*` | `go run ./cmd/api grant-role <email> platform_admin` (the account's address must be verified) |
| `mfa_required` on `/ops/*` | Turn on two-factor authentication (`POST /v1/auth/mfa/totp`, then `/confirm`), or sign in again with a code |
| `invalid_mfa` with a correct-looking code | The code was already used, or the phone's clock is off by more than 30 seconds: wait for the next code |
| `mfa_unavailable` | Set `AUTH_ENCRYPTION_KEYS` (`orb dev` does it in development) |
| Lost authenticator app and recovery codes | `go run ./cmd/api reset-mfa <email>` |
| Browser isn't kept signed in | Serve over HTTPS (or localhost), and call the API from the same site or an origin in `APP_CORS_ORIGINS` |
| `too_many_attempts` in tests | Limits are per address, per network, per user and per address's codes; use different addresses per test |
| `invalid_credentials` right after verifying | The address was registered more than once with different passwords before it was verified, so the account has none: use forgot password |
| `social_link_required` | Sign in with the password, then link the provider with `POST /v1/auth/identities` |
