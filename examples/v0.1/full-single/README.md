# acme-api

A Go API created with [gorbital](https://gorbital.dev) (Full preset, single-tenant).

## Run

```bash
orb dev      # PostgreSQL in Docker, the mail catcher, migrations, seed data, live reload
```

The first run prints the password of the seeded administrator, `admin@example.com`, once. Sign-in methods (email and password, authenticator apps, passkeys, Google, Apple and GitHub) and how to create the credentials for each are in [AUTH_PROVIDERS.md](AUTH_PROVIDERS.md); the app lists what's on when it starts. For passkeys, open the app at http://localhost:8080. Without the gorbital CLI:

```bash
cp .env.example .env            # then set AUTH_ENCRYPTION_KEYS: echo "k1:$(openssl rand -base64 32)"
docker compose up -d --wait     # PostgreSQL
set -a; . ./.env; set +a        # the app reads the environment, not .env: export it in each terminal
go run ./cmd/migrate            # database migrations
go run ./cmd/seed               # administrator and example projects (development only)
go run ./cmd/api
```

Commit the new app before running `orb gen` or `orb add`: they refuse to change an app with uncommitted changes.

| URL | What |
|---|---|
| http://127.0.0.1:8080/docs | Interactive API reference (in production only with `APP_DOCS_ENABLED=true`) |
| http://127.0.0.1:8080/openapi.json | OpenAPI 3.1 document (served with the docs) |
| http://127.0.0.1:8080/v1/ping | Example endpoint; its reply is a runtime setting |
| http://127.0.0.1:8080/ops/settings | Runtime settings (platform role) |
| http://127.0.0.1:8080/ops/jobs/definitions | Job configuration, run now, history (platform role) |
| http://127.0.0.1:8080/ops/audit | Audit log: who changed what, filterable (platform role) |
| http://127.0.0.1:8080/ops/mail | Email provider and sender; `POST /ops/mail/test` sends a test email (platform role) |
| http://127.0.0.1:3100/mail | The Dev Portal's Mail screen: every email sent in development |
| http://127.0.0.1:3000 | Grafana: traces, metrics and logs, with `orb dev --observability` |
| http://127.0.0.1:8080/livez · /readyz | Health checks |

## Sign in as the administrator

Seed data creates `admin@example.com` with the `platform_admin` role and two-factor authentication on, because ops roles require it. The first `orb dev` (or `go run ./cmd/seed`) prints its password, 2FA key and recovery codes once; nothing is saved. Add the 2FA key to an authenticator app, then sign in in two steps:

```bash
CHALLENGE=$(curl -s -X POST http://127.0.0.1:8080/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"admin@example.com","password":"<the printed password>"}' | jq -r .mfa.challenge_token)
TOKEN=$(curl -s -X POST http://127.0.0.1:8080/v1/auth/login/mfa -H 'Content-Type: application/json' \
  -d "{\"challenge_token\":\"$CHALLENGE\",\"code\":\"<code from the app>\",\"transport\":\"bearer\"}" | jq -r .token)
curl http://127.0.0.1:8080/ops/settings -H "Authorization: Bearer $TOKEN"
```

Lost the password? `POST /v1/auth/password/forgot` and the code from the Dev Portal's Mail screen. Lost the authenticator app? Send a recovery code as `"recovery_code"` instead of `"code"`, or run `go run ./cmd/api reset-mfa admin@example.com`.

To make your own account an administrator:

```bash
curl -X POST http://127.0.0.1:8080/v1/auth/register -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"a long enough password"}'
# read the 6-digit code in the Dev Portal's Mail screen (http://127.0.0.1:3100/mail), then:
curl -X POST http://127.0.0.1:8080/v1/auth/verify-email -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","code":"123456"}'
go run ./cmd/api grant-role you@example.com platform_admin
TOKEN=$(curl -s -X POST http://127.0.0.1:8080/v1/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"a long enough password","transport":"bearer"}' | jq -r .token)
# ops roles need two-factor authentication: set up an authenticator app, then confirm a code
curl -X POST http://127.0.0.1:8080/v1/auth/mfa/totp -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"password":"a long enough password"}'      # the secret and otpauth:// URI for the app
curl -X POST http://127.0.0.1:8080/v1/auth/mfa/totp/confirm -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"code":"<code from the app>"}'              # recovery codes; this session can now use /ops
curl http://127.0.0.1:8080/ops/settings -H "Authorization: Bearer $TOKEN"
```

Browsers sign in without `"transport"` and get an HttpOnly session cookie instead. See the gorbital authentication guide for sign-in flows, roles and limits.

## Configuration

Two layers:

| Layer | Holds | Change it |
|---|---|---|
| Environment ([.env.example](.env.example)) | Secrets and infrastructure: database URL, tokens, the Resend API key or SMTP login, addresses | Edit env and restart |
| Runtime settings and job definitions | Tunables such as the ping reply, the email sender name and address, job schedules, timeouts and retries | `PUT /ops/settings/{key}`, `PUT /ops/jobs/definitions/{name}`; live on every instance |

## Email

This app sends email with **Resend**. In development every email goes to orb dev's mail catcher instead (the Dev Portal's Mail screen), so no key is needed to start.

1. For real email, create a key at https://resend.com/api-keys and set `RESEND_API_KEY` in `.env`.
2. Set the sender: `PUT /ops/settings/mail.from_email` and `PUT /ops/settings/mail.from_name`.
3. Check it: `POST /ops/mail/test` with `{"to":"you@example.com"}`.

Prefer SMTP (Amazon SES, Postmark, Mailgun, your own server)? Run `orb add mail` and choose it. See the gorbital email guide for providers and delivery.

```bash
curl -X PUT http://127.0.0.1:8080/ops/settings/example.ping_message \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"value":"hello","version":0,"reason":"demo"}'
```

## Common tasks

| Task | Command |
|---|---|
| Run tests (needs `docker compose up -d --wait`) | `GORBITAL_TEST_DATABASE_URL=postgres://acme-api:acme-api@127.0.0.1:5432/acme-api?sslmode=disable go test ./...` |
| Export the OpenAPI document, Postman collection and llms.txt | `go run ./cmd/api openapi --dir api` |
| Record new public names (error codes, audit actions, permissions, settings, jobs) in `api/surface.json` | `go test ./internal/app -run TestPublicSurface -update` |
| Add a background job | `orb gen job <Name>` (asks the rest), or with flags: `orb gen job CleanupSessions --schedule "0 3 * * *" --yes` |
| Change a job's schedule, timeout or retries | `PUT /ops/jobs/definitions/{name}` (no redeploy) |
| See who changed a setting or job | `GET /ops/audit?resource_id=<key or name>` |
| Switch email provider (Resend or SMTP) | `orb add mail` |
| Turn off an account's two-factor authentication (lost authenticator app and recovery codes) | `go run ./cmd/api reset-mfa <email>` |
| Replace the 2FA encryption key | Put the new key first in `AUTH_ENCRYPTION_KEYS` on every instance, run `go run ./cmd/api rotate-auth-keys`, then remove the old key |
| Turn maintenance mode on or off when `/ops` can't be reached | `go run ./cmd/api maintenance on --message "Back soon"`, then `go run ./cmd/api maintenance off` |
| Run tests with email delivery checks | also set `GORBITAL_TEST_MAILPIT_SMTP=127.0.0.1:1025 GORBITAL_TEST_MAILPIT_URL=http://127.0.0.1:8025` |
| Build a container | `docker build -t acme-api .` |

## Examples to keep or remove

The app starts with four examples that show the patterns the rest of the code follows. Keep them as references, or remove them:

| Example | To remove |
|---|---|
| Seed data (`go run ./cmd/seed`) | Delete `cmd/seed/`, `internal/app/seed.go` and `internal/app/seed_test.go`; `orb dev` skips seed data when `cmd/seed` is missing |
| `projects` resource (`/v1/projects`) | Delete `internal/modules/projects/`, `internal/app/module_projects.go` and `internal/app/projects_test.go`, its line in `internal/app/modules.go`, and the example projects in `internal/app/seed.go`. If its migration already ran, add a migration that drops the `projects` table |
| `heartbeat` job | Delete `internal/jobs/heartbeat/` and `internal/app/job_heartbeat.go`, and its line in `internal/app/jobs.go` |
| `ping` endpoint (`/v1/ping`) | Delete `internal/modules/ping/` and `internal/app/module_ping.go`, its line and the `pingMessage` field in `internal/app/modules.go`, and the `example.ping_message` setting in `internal/app/settings.go`; the tests that call `/v1/ping` or change that setting use it too |

Then run `go run ./cmd/api openapi --dir api` and `go test ./...`.

## Project layout

See [ARCHITECTURE.md](ARCHITECTURE.md). AI coding assistants: see [AGENTS.md](AGENTS.md).
