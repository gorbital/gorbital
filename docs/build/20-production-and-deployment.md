# 20. Production and deployment

Plateful in production is one container image and one PostgreSQL database. There is no separate worker process, no scheduler to install, no Redis, no message broker: every instance serves HTTP and works jobs, and River elects one leader among them so a periodic job — `orders_late_sweep`, `incidents_detect` — runs once however many instances there are.

This chapter is the shape of a deployment and the handful of settings that are easy to get wrong. The [production guide](../guides/production.md) is the reference: it covers the rolling-deploy rules, TLS and proxies, Prometheus metrics, backups and the full "never do this" list. Read it once before your first deploy; this chapter will point you into it rather than repeat it.

## The image

**What we're doing.** Turning the repository into one static binary in a container that has no shell.

**Why.** A distroless image with a single static binary has almost nothing in it to exploit, nothing to drift, and nothing to install at start. It also means debugging is done through logs, traces and `/ops` — which is the whole reason [chapter 18](18-audit-logs-and-observability.md) exists.

**What the framework already gives us.** `orb new` wrote this file, and `gorbital.Main` gives the binary its subcommands, so one image serves the API *and* runs the migrations *and* grants roles.

**What we build ourselves.** Nothing. Plateful's `Dockerfile` is the one `orb new` wrote in [chapter 1](01-create-the-app.md).

**How.**

<!-- include examples/apps/plateful/Dockerfile#dockerfile -->

```bash
docker build --build-arg VERSION=$(git describe --tags --always) -t plateful:$(git rev-parse --short HEAD) .
```

**What just happened.** `VERSION` is linked into the binary at `gorbital.dev/buildinfo.version`, so it shows up in `GET /version`, on every log line, on traces, and in `GET /ops/releases/instances` — which is how you answer "is the fix actually running yet?". The runtime image sets `APP_ENV=production` and `APP_ADDR=0.0.0.0:8080` and runs as `nonroot`. The entrypoint is the binary itself, so `docker run plateful:<tag> migrate` runs the migration command and `docker run plateful:<tag> grant-role ada@example.com platform_admin` grants a role, both with the production environment and neither needing a shell.

Seed data is not in the image, and the `seed` command refuses to run when `APP_ENV` is production. Administrators are made with `grant-role` on a verified account, and nothing else.

## The environment a production Plateful needs

**What we're doing.** Listing the variables without which the app will not start, or will start wrong.

**Why.** `gorbital.LoadConfig` checks every variable before anything connects and reports **all** the problems at once under `invalid configuration:`, exiting with code 2. That is a good failure — but only if you know what it is asking for.

**What the framework already gives us.** The checks, the `_FILE` variants for every secret, and the production rules: https-only origins, Mailpit refused, `/docs` off, `AUTH_ENCRYPTION_KEYS` required.

**What we build ourselves.** Plateful's `.env.example` documents every variable with a comment; production values come from your platform's secret store, never from a file in the repository.

**How.** The minimum for Plateful:

```bash
APP_ENV=production                                   # the Dockerfile sets it; the app refuses to start without it
DATABASE_URL_FILE=/run/secrets/database_url          # sslmode=require or verify-full
AUTH_ENCRYPTION_KEYS_FILE=/run/secrets/auth_keys
RESEND_API_KEY_FILE=/run/secrets/resend_api_key      # Plateful's mailer; see cmd/api/mail.go
APP_PUBLIC_URL=https://api.plateful.example
APP_CORS_ORIGINS=https://app.plateful.example        # https only in production
APP_TRUSTED_PROXIES=10.0.0.0/8                       # your load balancer's range
APP_REQUEST_TIMEOUT=30s
WEBAUTHN_RP_ID=plateful.example
WEBAUTHN_ORIGINS=https://app.plateful.example
STORAGE_DRIVER=s3                                    # with STORAGE_ENDPOINT, REGION, BUCKET and keys
```

Plus `AUTH_DEFAULT_RETURN_TO` if you use Google, Apple or GitHub sign-in, and `OTEL_EXPORTER_OTLP_ENDPOINT` or `METRICS_ADDR` if you ship telemetry. After the first deploy, set the runtime settings that are *not* environment variables — at least `mail.from_email`, which logs a warning while it is the default, and `orgs.invitation_url`, which the restaurant invitation email needs. Every variable, with its default and its validation, is in [environment variables](../guides/environment-variables.md).

**What just happened.** A deployment that forgets one of these does not start half-configured; it prints every missing value and exits 2. That is the behaviour you want in a release pipeline.

<a id="auth-encryption-keys"></a>

### `AUTH_ENCRYPTION_KEYS`

This one gets its own paragraph because losing it is unrecoverable.

It is a comma-separated list of `id:base64` entries, each exactly 32 bytes after decoding. The first key encrypts; all of them decrypt. It encrypts the two-factor secrets of every account that has an authenticator app — including the platform operators, whose roles *require* a second factor to use `/ops`.

```bash
echo "k1:$(openssl rand -base64 32)"
```

- Back it up **separately from the database**. A restored database without the key is a database in which no operator can pass their second factor.
- Rotate by putting a new key first and keeping the old one: `AUTH_ENCRYPTION_KEYS=k2:…,k1:…`, then run `/api rotate-auth-keys` in the image, then remove `k1` once it has finished. Removing the old key first breaks every unrotated secret.
- Never reuse a development key. `orb dev` writes a random one into `.env` for you, and that is a development key.

The [secrets and keys guide](../guides/secrets-and-keys.md#auth_encryption_keys) has the rest.

<a id="ops-allowed-ips"></a>

### `OPS_ALLOWED_IPS` — and the order that matters

**What we're doing.** Limiting `/ops/` to the addresses your operators come from.

**Why.** `/ops` is already behind sign-in, a platform role and a second factor. An IP allow list is the layer in front of all of that: a request from the wrong network gets 403 `ip_not_allowed` before the sign-in check, the guards and any input parsing.

**How — and this is the trap.** The address it matches is the client's address *after* `APP_TRUSTED_PROXIES` has been applied.

```bash
APP_TRUSTED_PROXIES=10.0.0.0/8       # first: your load balancer's range
OPS_ALLOWED_IPS=10.8.0.0/16          # then: your VPN's range
```

> **Don't do this:** set `OPS_ALLOWED_IPS` on an app behind a load balancer whose range is not in `APP_TRUSTED_PROXIES`.
>
> **What happens:** without `APP_TRUSTED_PROXIES`, the app takes every request's address to be the load balancer's own. That address is not in your VPN range, so **every** `/ops` request gets 403 `ip_not_allowed`, including yours — and `/ops` is where you would go to fix things. You have locked yourself out of the operations API of a running production app, and the only way back is a redeploy.
>
> **Do this instead:** set `APP_TRUSTED_PROXIES` first, deploy, confirm that audit events and logs show real client addresses rather than the balancer's, and only then add `OPS_ALLOWED_IPS`.

The same ordering matters for rate limits and audit events, which is why `APP_TRUSTED_PROXIES` belongs in your first production deploy whether or not you ever use the allow list. Do not list ranges that clients can reach directly: a trusted peer can choose its own `X-Forwarded-For`.

### `APP_REQUEST_TIMEOUT`

A handler that has not started its response within this duration gets 503 `request_timeout` and its context is cancelled, which cancels the SQL it is running. The default is 30 s; it must be shorter than the server's 60 s write timeout, and `0` means no deadline at all.

Plateful sets `30s`. The one route that legitimately takes longer — the daily summary, which is a real aggregate query — carries its own `gorbital.Timeout` rather than raising the global value. That is the right shape: one slow route should not give every route permission to hang.

`orb doctor` fails the app when `APP_REQUEST_TIMEOUT` is not a duration or is not shorter than 60 s, and warns when it is `0`.

<a id="migrations-are-a-release-step"></a>

## Migrations are a release step

**What we're doing.** Applying the schema before the new code starts, from a one-off run, never from an app instance.

**Why.** Three reasons, and the first is the one people learn the hard way. If instances migrated at start, a rolling deploy of four instances would run the migration four times concurrently. If a migration failed, you would have a partly-started deployment instead of a failed release step. And a migration would run with the permissions and the timeouts of a serving process rather than of a deliberate operation.

**What the framework already gives us.** `gorbital` **never runs migrations at start** ([ADR-0017](../adr/0017-application-lifecycle.md)). What it does instead is *warn*:

```text
the database has pending migrations: run the migrate command
```

with `pending`, `current` and `latest`. The app starts anyway and serves requests against a schema that does not have what the code expects — which is why this warning belongs in your alerting, and why `GET /ops/system` reports `migrations.pending` for exactly this purpose.

The `migrate` command takes a PostgreSQL advisory lock, so two concurrent runs apply each migration once; it applies the library's, the modules' and `db/migrations` ([chapter 6](06-migrations-and-the-database.md)) in one merged history, then River's.

**What we build ourselves.** One step in the pipeline.

**How.**

```bash
# 1. build and push
docker build --build-arg VERSION=$(git describe --tags --always) -t plateful:$TAG .

# 2. migrate, with the production environment, before any new instance starts
docker run --rm --env-file prod.env plateful:$TAG migrate

# 3. roll the instances
```

> **Don't do this:** call `gorbital.Migrate` from `main.go`, or add a container entrypoint that runs `migrate && serve`.
>
> **Do this instead:** a release step. If your platform has no pre-deploy hook, run the `migrate` container manually or from CI before the rollout, and treat a non-zero exit as a failed release.

**What just happened.** The schema is now ahead of the code, which is the only safe direction during a rolling deploy: old instances are still serving while new ones start. That is why migrations are **expand, then contract** — add a nullable column, deploy code that writes it, backfill, and only make it `NOT NULL` in a later release. Never rename or drop a column the previous release still reads. Migrations are forward-only; a bad one is fixed by a new one. Long-running index builds go in their own migration with `CREATE INDEX CONCURRENTLY` between `-- +goose NO TRANSACTION` annotations. The [production guide](../guides/production.md#migrate) has the details.

## Health checks

Two endpoints, two different questions, and using the wrong one is how a deploy takes the site down.

| Endpoint | Answers | Point your platform's… |
|---|---|---|
| `GET /livez` | Is this process alive? | **Liveness** probe. A failure means restart the container |
| `GET /readyz` | Can it serve traffic — is PostgreSQL reachable, and is it not shutting down? | **Readiness** probe, and the load balancer's health check |

`/readyz` is what makes a graceful shutdown work. On SIGTERM an instance marks itself **not ready** first, waits five seconds so the load balancer notices and stops routing to it, then stops accepting connections, lets in-flight requests finish, stops fetching jobs and lets running ones finish. Give the platform a stop grace period of at least 30 seconds, or it will kill the container in the middle of that.

Concurrent `/readyz` requests share one check, reused for a second, so a fleet of probes does not hammer the database. Both endpoints are public: block them at the load balancer if you would rather the internet not have them. The same goes for `/version`, which tells anyone the build version, Go version and commit.

## `/docs` and `/openapi.json` are off in production

`APP_DOCS_ENABLED` defaults to `true` in development and **`false` in production**. With it off, both `/docs` and the OpenAPI document answer 404, and a production Plateful serves no HTML at all.

That is the default because an API reference is a map of your attack surface, and most APIs are not public. Plateful's `api/openapi.json` is committed to the repository and exported by `go run ./cmd/api openapi --dir api`, which reads no environment variables at all — so partners and client generators can have the document without the server serving it.

Set `APP_DOCS_ENABLED=true` deliberately, when you want a public API reference, and know that you are turning on the one HTML page the app has.

## Before the first deploy

A short list, drawn from the production guide's longer one:

- Two or more instances, so a rolling deploy has somewhere to roll.
- `instances × APP_DB_MAX_CONNS` (10 each by default), plus migrations and River's listener, below PostgreSQL's `max_connections`.
- `APP_MAX_BODY_BYTES` above `images.max_bytes`, or a legal menu photo ([chapter 7](07-the-menu-money-and-photos.md)) is refused with 413 before the images module ever sees it. Plateful's `.env.example` says why in place:

<!-- include examples/apps/plateful/.env.example#max-body-bytes -->

- Alerts on: `/readyz` failures, the 5xx rate, `unhandled error` and `panic recovered` log lines, `incident opened: error rate above threshold`, the pending-migrations warning, discarded or repeatedly failing jobs — especially `gorbital.mail.send` and Plateful's own `notification_delivery` ([chapter 17](17-extending-the-framework.md)) — and database connection saturation.
- Point-in-time recovery on the database, tested, and `AUTH_ENCRYPTION_KEYS` backed up somewhere else.

## Next

[21. Troubleshooting](21-troubleshooting.md): what breaks first, and how to find out why with the tools you already have.
