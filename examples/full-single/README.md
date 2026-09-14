# acme-api

A Go API created with [apistock](https://apistock.dev) (Full preset, single-tenant).

> Work in progress: authentication, email and audit storage arrive later in v0.2. Until then the admin APIs use `OPS_TOKEN`.

## Run

```bash
cp .env.example .env            # then set OPS_TOKEN (openssl rand -hex 32)
docker compose up -d --wait     # PostgreSQL
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
| http://127.0.0.1:8080/livez · /readyz | Health checks |

## Configuration

Two layers:

| Layer | Holds | Change it |
|---|---|---|
| Environment ([.env.example](.env.example)) | Secrets and infrastructure: database URL, tokens, addresses | Edit env and restart |
| Runtime settings and job definitions | Tunables such as the ping reply, job schedules, timeouts and retries | `PUT /ops/settings/{key}`, `PUT /ops/jobs/definitions/{name}`; live on every instance |

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
| Build a container | `docker build -t acme-api .` |

## Project layout

See [ARCHITECTURE.md](ARCHITECTURE.md). AI coding assistants: see [AGENTS.md](AGENTS.md).
