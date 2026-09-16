# Running in production

How to build, configure, migrate, run and operate a Full app on servers. gorbital doesn't host anything: the app is one container image and PostgreSQL, deployable on any platform that runs containers. Beginners: start with the [go-live checklist](../sign-in/go-live.md).

## What runs

```text
            HTTPS
clients ───────────► load balancer / proxy (TLS)
                        │   health: GET /readyz
                        ▼
              ┌──────────────────────┐   ┌──────────────────────┐
              │ app instance  /api   │   │ app instance  /api   │   … N instances, identical
              │  HTTP server         │   │  HTTP server         │
              │  River job workers   │   │  River job workers   │
              │  settings listener   │   │  settings listener   │
              │  release tracker     │   │  release tracker     │
              └──────────┬───────────┘   └──────────┬───────────┘
                         └─────────────┬────────────┘
                                       ▼
                                  PostgreSQL          data, sessions, settings, job queue, audit log
                                       ▲
                      /migrate ────────┘   one-off, before each release

  outbound: Resend or SMTP · Google · Apple · OTLP endpoint (optional)
```

There's no separate worker process: every instance serves HTTP and works jobs. River elects one leader among instances for periodic jobs, so a schedule runs once however many instances there are. Runtime settings changed on one instance reach the others through PostgreSQL `LISTEN/NOTIFY`, with a periodic resync.

## Build the image

The app's `Dockerfile` builds a static binary and copies it into a distroless image that runs as a non-root user:

```bash
docker build --build-arg VERSION=$(git describe --tags --always) -t acme-api:$(git rev-parse --short HEAD) .
```

| Stage | Base | Contents |
|---|---|---|
| build | `golang:1.26` | `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X gorbital.dev/buildinfo.version=${VERSION}"` of `./cmd/api` and `./cmd/migrate` |
| runtime | `gcr.io/distroless/static-debian12:nonroot` | `/api` (entrypoint), `/migrate`; `APP_ENV=production`, `APP_ADDR=0.0.0.0:8080`, port 8080, user `nonroot` |

`VERSION` appears in `GET /version`, logs, traces and `/ops/releases`. The image has no shell; debug with logs, traces and the ops API.

`cmd/seed` isn't in the image: seed data is for development and refuses to run in production.

## Configure

Set environment variables through your platform's secret store; see [environment variables](environment-variables.md) for every one and [secrets and keys](secrets-and-keys.md) for handling. The minimum:

```bash
DATABASE_URL_FILE=/run/secrets/database_url        # sslmode=require or verify-full
AUTH_ENCRYPTION_KEYS_FILE=/run/secrets/auth_encryption_keys
RESEND_API_KEY_FILE=/run/secrets/resend_api_key    # or the SMTP_ variables
APP_ENV=production                                 # the Dockerfile sets it; the app refuses to start without it
APP_CORS_ORIGINS=https://app.example.com           # https only in production
APP_TRUSTED_PROXIES=10.0.0.0/8                     # your load balancer's range
WEBAUTHN_RP_ID=example.com
WEBAUTHN_ORIGINS=https://app.example.com
```

Plus `APP_PUBLIC_URL` and the provider variables for Google or Apple. `/docs` and the OpenAPI document are off in production; set `APP_DOCS_ENABLED=true` for a public API reference. The app refuses to start when production requirements aren't met, listing every problem.

After the first deploy, set runtime settings through the ops API: at least `mail.from_email` (the default `no-reply@example.com` logs a warning at start), and `orgs.invitation_url` in multi-tenant apps.

## Migrate

Migrations never run at app start ([ADR-0017](../adr/0017-application-lifecycle.md)). Run them as a release step, with the same environment, before new instances start:

```bash
docker run --rm --env-file prod.env --entrypoint /migrate acme-api:<tag>
```

- `/migrate` applies the app's goose migrations, then River's, and exits.
- It takes a PostgreSQL advisory lock, so two concurrent runs apply each migration once.
- Migrations are forward-only. There are no down migrations: fix a bad migration with a new one.
- **Expand, then contract.** Old instances keep running during a rolling deploy, so a release's migration must work with the previous release's code: add a nullable column, deploy code that writes it, backfill, then make it `NOT NULL` in a later release. Never rename or drop a column that running code still reads.
- Long-running changes (an index on a big table) belong in their own migration with `CREATE INDEX CONCURRENTLY`, between `-- +goose NO TRANSACTION` annotations.

## Run

| Setting | Recommended |
|---|---|
| Instances | 2 or more, for rolling deploys |
| Health checks | Readiness: `GET /readyz` (checks PostgreSQL; fails during shutdown; concurrent requests share one check, reused for 1 s). Liveness: `GET /livez`. Both are public: block them at the load balancer for internet clients if you can |
| Stop grace period | At least 30 s: 5 s drain, then up to 25 s for requests and jobs |
| Resources | Start with 0.5 CPU and 256 MiB per instance and adjust from metrics |
| Database connections | `instances × APP_DB_MAX_CONNS` (default 10) plus migrations and River's listener, below PostgreSQL's `max_connections` |
| Job concurrency | `APP_JOB_WORKERS` per instance (default 10) |

On SIGTERM an instance: marks itself not ready, waits 5 s so the load balancer stops routing to it, stops accepting connections and lets in-flight requests finish, stops fetching jobs and lets running jobs finish within the timeout, records a clean stop in `release_instances`, then closes the database pool and flushes telemetry.

## TLS, proxies and client IPs

- Terminate TLS at the load balancer; the app speaks plain HTTP on 8080. HSTS is sent in production, so serve the API only over https.
- Session cookies are `__Host-` cookies with `Secure`; browsers only send them over https (and `localhost`).
- Set `APP_TRUSTED_PROXIES` to your load balancers' ranges. The app then takes the client's address from `X-Forwarded-For` on requests from those addresses only, for per-IP rate limits, logs, traces and audit events ([ADR-0052](../adr/0052-shared-rate-limits.md)). Don't add your own `X-Forwarded-For` middleware: one that trusts every peer lets clients choose their IP.
- Rate limits are stored in PostgreSQL and shared by every instance ([ADR-0052](../adr/0052-shared-rate-limits.md)).
- Clients can't choose their request ID or trace: the app generates `X-Request-ID` and starts a new trace for each request, linking any incoming `traceparent`. Set `APP_TRUSTED_CALLERS` to the ranges of gateways and internal services whose request IDs and traces should carry on. List client addresses, not your load balancer, unless the load balancer itself sets those headers and drops clients' values.

## Observe

| Signal | Where |
|---|---|
| Logs | JSON on stdout in production, one line per request with `request_id`, `trace_id`, `span_id`. Collect with your platform |
| Traces and metrics | Set `OTEL_EXPORTER_OTLP_ENDPOINT` (and `OTEL_EXPORTER_OTLP_HEADERS` for vendor auth). HTTP server spans, every SQL query, job execution |
| Health | `/livez`, `/readyz`. `/version` shows the build version, Go version and commit to anyone; block it at the load balancer if that matters to you |
| Releases | `GET /ops/releases/current`, `/ops/releases/instances`: which versions are running where |
| Jobs | `GET /ops/jobs/runs`, `/ops/queues`; retry, cancel, pause queues |
| Audit | `GET /ops/audit`: who changed settings, jobs, roles and accounts |
| Sign-in methods | `GET /ops/auth/providers`, or `/api auth-providers` in the container |

Alert on: `/readyz` failures, 5xx rate, `unhandled error` and `panic recovered` log lines, discarded or repeatedly failing jobs (especially `gorbital.mail.send`), and database connection saturation.

## Operate

Commands in the image, run with the production environment (for example `docker run --rm --env-file prod.env --entrypoint /api acme-api:<tag> <command>`):

| Command | Does |
|---|---|
| `/api roles` | Lists platform roles and permissions |
| `/api grant-role <email> platform_admin` | Grants a role; the user must set up 2FA to use ops endpoints |
| `/api revoke-role <email> <role>` | Revokes it |
| `/api reset-mfa <email>` | Turns off a user's authenticator app and recovery codes |
| `/api rotate-auth-keys` | Re-encrypts TOTP secrets with the first key in `AUTH_ENCRYPTION_KEYS` |
| `/api auth-providers` | Shows which sign-in methods are configured |
| `/api openapi` | Prints the OpenAPI document (it reads no environment variables) |

## Back up

Everything durable is in PostgreSQL. Use your provider's point-in-time recovery, and test restores. Keep `AUTH_ENCRYPTION_KEYS` backed up separately: a restored database is useless for TOTP without it.

Account and organisation deletion is soft first: data is purged by jobs after `auth.deleted_account_retention` (30 days by default) and `orgs.deleted_org_retention`.

## Never do this in production

- Set `MAIL_DELIVERY=mailpit`, or copy a development `.env`. (The app refuses the first.)
- Reuse the development `AUTH_ENCRYPTION_KEYS`, or lose the production one.
- Run `cmd/seed`, or create administrators any way other than `grant-role` on a verified account.
- Run migrations from app instances at start, or edit a migration that has already run anywhere.
- Deploy a migration that breaks the previous release's code during a rolling deploy.
- Expose the app's port directly without TLS, or serve it on both http and https.
- Put secrets in runtime settings, image layers, build args or the repository.
- Change `WEBAUTHN_RP_ID` after users have passkeys: every passkey stops working.
- Remove an old key from `AUTH_ENCRYPTION_KEYS` before `rotate-auth-keys` has finished.
- Set `APP_CORS_ORIGINS` to origins you don't control: they're also trusted for cross-origin requests and as sign-in `return_to` targets.
- Leave the Google consent screen in Testing, or forget the production redirect URI.
- Run behind a load balancer without `APP_TRUSTED_PROXIES`: every client shares the balancer's per-IP rate limit, and logs and audit events show its address. Don't list ranges clients can connect from directly: they could choose their own IP.
