# acme-api

A Go API created with [apistock](https://apistock.dev) (Full preset, single-tenant).

> Work in progress: authentication arrives later in v0.2. Until then the admin APIs use `OPS_TOKEN`.

## Run

```bash
cp .env.example .env            # then set OPS_TOKEN (openssl rand -hex 32)
docker compose up -d --wait     # PostgreSQL and Mailpit
go run ./cmd/migrate            # database migrations
go run ./cmd/api                # or: aps dev
```

| URL | What |
|---|---|
| http://127.0.0.1:8080/docs | Interactive API reference |
| http://127.0.0.1:8080/openapi.json | OpenAPI 3.1 document |
| http://127.0.0.1:8080/v1/ping | Example endpoint; its reply is a runtime setting |
| http://127.0.0.1:8080/ops/settings | Runtime settings (bearer `OPS_TOKEN`) |
| http://127.0.0.1:8080/ops/jobs/definitions | Job configuration, run now, history (bearer `OPS_TOKEN`) |
| http://127.0.0.1:8080/ops/audit | Audit log: who changed what, filterable (bearer `OPS_TOKEN`) |
| http://127.0.0.1:8080/ops/mail | Email provider and sender; `POST /ops/mail/test` sends a test email (bearer `OPS_TOKEN`) |
| http://127.0.0.1:8025 | Mailpit: every email sent in development |
| http://127.0.0.1:8080/livez · /readyz | Health checks |

## Configuration

Two layers:

| Layer | Holds | Change it |
|---|---|---|
| Environment ([.env.example](.env.example)) | Secrets and infrastructure: database URL, tokens, the Resend API key or SMTP login, addresses | Edit env and restart |
| Runtime settings and job definitions | Tunables such as the ping reply, the email sender name and address, job schedules, timeouts and retries | `PUT /ops/settings/{key}`, `PUT /ops/jobs/definitions/{name}`; live on every instance |

## Email

This app sends email with **Resend**. In development every email goes to Mailpit instead (http://127.0.0.1:8025), so no key is needed to start.

1. For real email, create a key at https://resend.com/api-keys and set `RESEND_API_KEY` in `.env`.
2. Set the sender: `PUT /ops/settings/mail.from_email` and `PUT /ops/settings/mail.from_name`.
3. Check it: `POST /ops/mail/test` with `{"to":"you@example.com"}`.

Prefer SMTP (Amazon SES, Postmark, Mailgun, your own server)? Run `aps add mail` and choose it. See the [email guide](../../docs/guides/email.md).

```bash
curl -X PUT http://127.0.0.1:8080/ops/settings/example.ping_message \
  -H "Authorization: Bearer $OPS_TOKEN" -H 'Content-Type: application/json' \
  -d '{"value":"hello","version":0,"reason":"demo"}'
```

## Common tasks

| Task | Command |
|---|---|
| Run tests (needs `docker compose up -d --wait`) | `APISTOCK_TEST_DATABASE_URL=postgres://acme:acme@127.0.0.1:5432/acme?sslmode=disable go test ./...` |
| Export the OpenAPI document | `go run ./cmd/api openapi > api/openapi.json` |
| Add a background job | `aps gen job <Name>` (asks the rest), or with flags: `aps gen job CleanupSessions --schedule "0 3 * * *" --yes` |
| Change a job's schedule, timeout or retries | `PUT /ops/jobs/definitions/{name}` (no redeploy) |
| See who changed a setting or job | `GET /ops/audit?resource_id=<key or name>` |
| Switch email provider (Resend or SMTP) | `aps add mail` |
| Run tests with email delivery checks | also set `APISTOCK_TEST_MAILPIT_SMTP=127.0.0.1:1025 APISTOCK_TEST_MAILPIT_URL=http://127.0.0.1:8025` |
| Build a container | `docker build -t acme-api .` |

## Project layout

See [ARCHITECTURE.md](ARCHITECTURE.md). AI coding assistants: see [AGENTS.md](AGENTS.md).
