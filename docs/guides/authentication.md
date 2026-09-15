# Authentication guide

How a Full preset app signs people up and in, keeps them signed in, and decides what they may do. Implemented in `examples/full-single` (`internal/modules/auth`) on the building blocks of `modules/auth`. Decisions: [ADR-0024](../adr/0024-authentication-methods.md), [ADR-0038](../adr/0038-authentication-v0-2.md).

## The flow

```text
register ──► email with a 6-digit code ──► verify-email ──► login ──► session (cookie or token)
                                                                         │
                                         /v1/auth/me, /ops/* (with a role), your endpoints
```

Start the app with Docker running (`docker compose up -d --wait`, `go run ./cmd/migrate`, `go run ./cmd/api`). Emails land in Mailpit at http://127.0.0.1:8025.

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

Your app owns authentication like any other module, with all four layers. The apistock library only supplies the security building blocks.

| Folder | What's in it |
|---|---|
| `internal/modules/auth/domain/` | `User`, `Session`, `Code` and the module's errors |
| `internal/modules/auth/usecase/` | The flows: `register.go`, `verify_email.go`, `login.go`, `sessions.go` (authenticate, sessions, logout), `password.go` (reset and change), `account.go` (delete, create user, roles, cleanup); `ports.go` lists what they need from storage |
| `internal/modules/auth/repository/` | `store.go` with transactions, and one SQL file per operation: `insert_user.go`, `select_user.go`, `insert_session.go`, `revoke_session.go`, `insert_code.go`, … |
| `internal/modules/auth/delivery/` | The `/v1/auth` endpoints |
| `internal/app/permissions.go` | Permissions and platform roles |
| `internal/app/admin.go` | The `grant-role`, `revoke-role` and `roles` commands |
| `db/migrations/…_auth.sql` | The `auth_users`, `auth_sessions`, `auth_codes` and `auth_user_roles` tables |
| `apistock.dev/modules/auth` (library) | Password hashing, tokens, codes, cookies, the request middleware, the permission catalog, plain emails |

Change a rule, such as allowing only your company's email domain, in the use case (`register.go`); add a column with a new migration and a repository file.

## Your first administrator

`/ops/*` needs a platform role. In development, seed data already created one: the first `aps dev` (or `go run ./cmd/seed`) creates `admin@example.com` with `platform_admin` and prints its random password once, without saving it ([ADR-0042](../adr/0042-development-seed-data.md)). Sign in with it, or reset it through `POST /v1/auth/password/forgot` and Mailpit.

To give your own account a role, in development or production, register and verify it as above, then grant the role from the app's directory:

```bash
go run ./cmd/api roles                                        # list roles and their permissions
go run ./cmd/api grant-role you@example.com platform_admin    # recorded in the audit log as "cli"
curl http://127.0.0.1:8080/ops/settings -H "Authorization: Bearer $TOKEN"
```

The role applies to your next request; there's no need to sign in again. `go run ./cmd/api revoke-role <email> <role>` takes it away.

| Role | Can |
|---|---|
| `platform_admin` | Everything under `/ops`: settings, jobs, audit log, email |
| `ops_viewer` | Read settings, jobs, the audit log and email status; change nothing |

Add roles and permissions in `internal/app/permissions.go`.

## Browsers and native apps

| Client | Login body | Result | Later requests |
|---|---|---|---|
| Browser (default) | `{"email", "password"}` | `Set-Cookie: __Host-session=…; Secure; HttpOnly; SameSite=Lax` | The browser sends the cookie; scripts can't read it |
| Mobile, desktop, CLI | `{"email", "password", "transport": "bearer"}` | `{"token": "…"}` in the body, shown once | `Authorization: Bearer <token>` |

- Browsers need HTTPS in production (the cookie is `Secure`); `localhost` works in development.
- Cross-site requests carrying the cookie are refused (403), so other websites can't act as your users. Set `APP_CORS_ORIGINS` for your own frontend's origin.
- Store native tokens in the platform's secure storage (Keychain, Keystore).

## Endpoints

| Method and path | Needs a session | Purpose | Success |
|---|---|---|---|
| `POST /v1/auth/register` | | Create an account; emails a code | 202 |
| `POST /v1/auth/verify-email` | | `{email, code}` | 204 |
| `POST /v1/auth/verify-email/resend` | | `{email}`; at most once a minute | 202 |
| `POST /v1/auth/login` | | `{email, password, transport?}` | 200 `{user, session, token?}` |
| `POST /v1/auth/password/forgot` | | `{email}`; emails a reset code | 202 |
| `POST /v1/auth/password/reset` | | `{email, code, password}`; signs out every device | 204 |
| `GET /v1/auth/me` | ✓ | User, current session, permissions | 200 |
| `PUT /v1/auth/password` | ✓ | `{current_password, new_password}`; signs out other devices | 204 |
| `GET /v1/auth/sessions` | ✓ | Signed-in devices, `current` marks this one | 200 |
| `DELETE /v1/auth/sessions/{id}` | ✓ | Sign out one device | 204 |
| `POST /v1/auth/logout` | ✓ | Sign out this device | 204 |
| `POST /v1/auth/logout-all` | ✓ | Sign out every device | 200 `{revoked}` |
| `DELETE /v1/auth/me` | ✓ | `{password}`; delete the account | 204 |

## What users see

- **Registration** always answers "check your email", even if the address already has an account; the owner of an existing account gets an email saying someone tried to sign up.
- **Wrong email or password** is one answer: `invalid_credentials`. `email_not_verified` appears only after the right password.
- **Forgot password** always answers "check your email".
- **Codes** are 6 digits, expire (15 minutes to verify, 30 to reset, adjustable), allow 5 tries, and a new one replaces the old one.
- **Passwords** need 12 to 128 characters; `weak_password` says what's wrong.
- **Too many attempts**: 10 logins per address per 15 minutes, 60 auth requests per minute per IP address; `too_many_attempts` says how long to wait.

## Sessions

- A session ends after 14 days without use or 90 days in total (runtime settings `auth.session_idle_ttl`, `auth.session_absolute_ttl`), or when signed out.
- Changing the password signs out other devices; resetting it or deleting the account signs out every device.
- The `auth_cleanup` job (daily, 03:30 UTC) removes ended sessions after 7 days, old codes, and deleted accounts after `auth.deleted_account_retention` (30 days).

## Settings

| Key | Default | Range | Reason required |
|---|---|---|---|
| `auth.session_idle_ttl` | 14 days | 1 hour – 90 days | Yes |
| `auth.session_absolute_ttl` | 90 days | 1 – 365 days | Yes |
| `auth.verification_code_ttl` | 15 minutes | 5 minutes – 1 hour | No |
| `auth.reset_code_ttl` | 30 minutes | 10 minutes – 2 hours | Yes |
| `auth.deleted_account_retention` | 30 days | 1 – 365 days | Yes |

Change them with `PUT /ops/settings/{key}`. The auth module also enforces hard limits of its own, so no setting can make sessions or codes unsafe.

## In your own code

Use cases get the signed-in user from the context; the middleware sets it for every request.

```go
import authlib "apistock.dev/modules/auth"

func (s *Service) CreateProject(ctx context.Context, name string) (Project, error) {
	a, ok := actor.From(ctx)
	if !ok || a.Kind == actor.KindAnonymous {
		return Project{}, ErrUnauthenticated
	}
	if !a.Can("projects.create") {
		return Project{}, ErrForbidden
	}
	p, _ := authlib.PrincipalFrom(ctx) // user, session and permissions, when you need more than the actor
	// …
}
```

1. Declare the permission in `internal/app/permissions.go` and add it to a role.
2. Check it in the use case with `actor.Can`.
3. Map your module's `ErrUnauthenticated` and `ErrForbidden` to `unauthenticated` (401) and `forbidden` (403) in `module_<name>.go`.

## Error codes

| Code | Status | When |
|---|---|---|
| `unauthenticated` | 401 | No valid session |
| `forbidden` | 403 | Signed in without the permission |
| `invalid_credentials` | 401 | Wrong email or password |
| `email_not_verified` | 403 | Right password, address not verified yet |
| `invalid_email` | 422 | Not an email address |
| `weak_password` | 422 | Password fails the policy; `detail` says why |
| `invalid_code` | 422 | Wrong, used or expired code |
| `too_many_attempts` | 429 | Rate limited; `detail` says how long to wait |
| `session_not_found` | 404 | Revoking a session that isn't yours or has ended |
| `auth_unavailable` | 503 | The session store couldn't be reached |

## Audit events

Every sign-in (successful or not), verification, password change or reset, sign-out, account deletion and role change is recorded with the client's IP address and user agent. See them with `GET /ops/audit?action_prefix=auth.`. Email addresses are never stored in event metadata.

## Troubleshooting

| Symptom | Fix |
|---|---|
| No code arrives | Check Mailpit (http://127.0.0.1:8025) in development, or `GET /ops/jobs/runs?kind=apistock.mail.send` for delivery errors ([email guide](email.md)) |
| `email_not_verified` | Verify with the emailed code, or `POST /v1/auth/verify-email/resend` |
| `forbidden` on `/ops/*` | `go run ./cmd/api grant-role <email> platform_admin` |
| Browser isn't kept signed in | Serve over HTTPS (or localhost), and call the API from the same site or an origin in `APP_CORS_ORIGINS` |
| `too_many_attempts` in tests | Limits are per address and per IP; use different addresses per test |
