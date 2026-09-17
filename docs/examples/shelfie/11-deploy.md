# 11. Deploy

Shelfie is finished: books, shelves, profiles, phone sign-in, book clubs, a partner's webhooks, and `/ops/` for the people who run it. This chapter puts it in front of real readers — the image, the environment, the migrations and the health checks — and then what to watch afterwards. The reference for every setting is [Running in production](../../guides/production.md); this is Shelfie's version of it, with the choices made.

## 1. One binary, one image

`cmd/api` is the whole app: the server and every command its modules add. The image builds that one binary and copies it into a distroless base, so there is no shell, no package manager and nothing running as root:

<!-- include examples/apps/shelfie/Dockerfile#dockerfile -->

```bash
docker build --build-arg VERSION="$(git rev-parse --short HEAD)" -t shelfie:$(git rev-parse --short HEAD) .
docker run --rm shelfie:<tag> help      # every command the app has
```

`VERSION` reaches `gorbital.dev/buildinfo`, and from there `GET /version`, every log line, every trace and `/ops/releases`, where an operator sees which build each instance runs. Tag images with the commit, never `latest`: `/ops/releases` is only useful if a version means one build.

## 2. The environment

Shelfie's `.env.example` is development. Production sets the same names through the platform's secret store, and the app **refuses to start** when something required is missing or unsafe, listing every problem at once instead of failing on the first request.

| Variable | Shelfie's value | Why |
|---|---|---|
| `APP_ENV` | `production` | The Dockerfile sets it. It turns on HSTS, `__Host-` cookies, the https-only CORS check, and turns off `/docs` |
| `DATABASE_URL_FILE` | `/run/secrets/database_url` | `sslmode=verify-full`. The `_FILE` form keeps the secret out of the process environment |
| `AUTH_ENCRYPTION_KEYS_FILE` | `/run/secrets/auth_encryption_keys` | Encrypts readers' authenticator-app secrets. Losing it locks every reader out of their second factor |
| `PARTNER_WEBHOOK_SECRET_FILE` | `/run/secrets/partner_webhook_secret` | Pagebound's signing secret ([chapter 10](10-hardening.md)); two, comma-separated, while it rotates |
| `APP_PUBLIC_URL` | `https://api.shelfie.example` | The links in emails and the OAuth redirect URLs |
| `APP_CORS_ORIGINS` | `https://shelfie.example` | The web app's origin, https only |
| `APP_TRUSTED_PROXIES` | the load balancer's range | Without it every request seems to come from the balancer, so per-IP rate limits, the `/ops` allow list and the audit log all see one address |
| `OPS_ALLOWED_IPS` | the VPN's range | `/ops/` from anywhere else is `403 ip_not_allowed` |
| `APP_REQUEST_TIMEOUT` | `30s` | Below the load balancer's idle timeout |
| `WEBAUTHN_RP_ID`, `WEBAUTHN_ORIGINS` | `shelfie.example`, `https://shelfie.example` | Passkeys are bound to the domain; a wrong value looks like a broken passkey |
| `RESEND_API_KEY_FILE` | `/run/secrets/resend_api_key` | Verification and reset emails. In development they go to the Dev Portal's Mail screen |
| `METRICS_ADDR` | `0.0.0.0:9464` | `GET /metrics` on a second listener, never on the public API |

After the first deploy, an operator sets the runtime settings `/ops/` owns rather than the environment: at least `mail.from_email`, and `books.shelf_limit` when the default doesn't suit ([chapter 5](05-operations.md)).

## 3. Migrations are a release step, not a start-up step

Shelfie never migrates when it boots ([ADR-0017](../../adr/0017-application-lifecycle.md)): two instances starting at once would race, and a rolling deploy would migrate under the previous version's feet. The pipeline runs them once, with the same environment, before any new instance starts:

```bash
docker run --rm --env-file prod.env shelfie:<tag> migrate --status   # what is pending
docker run --rm --env-file prod.env shelfie:<tag> migrate            # apply
```

The command applies gorbital's, the built-in modules' and Shelfie's own migrations in one history ordered by version, then River's job tables, and exits. It takes a PostgreSQL advisory lock, so two concurrent runs apply each migration once.

**Expand, then contract.** Old instances keep serving during a rolling deploy, so a release's migration must work with the *previous* release's code. Chapter 10's `partner_purchases` was a new table, which is always safe. Changing an existing one takes three releases: add the nullable column, deploy code that writes it, backfill, then make it `NOT NULL`. Never rename or drop a column that running code still reads. There are no down migrations — a bad migration is fixed by the next one.

## 4. The pipeline

Shelfie's workflow runs the same checks a developer runs, then proves the migrations apply to real data, then builds the image.

<!-- include examples/apps/shelfie/.github/workflows/ci.yml#test-job -->

Two details do the work. `GORBITAL_TEST_DATABASE_URL` points the suite at the service container: every test clones a database from a template migrated once from `db/migrations`, so there is no migrate step and tests don't share state ([chapter 4](04-tests.md)). `GORBITAL_REQUIRE_DB=1` turns a missing database from "skip the database tests" into a failure — without it a broken service container looks like a green build.

The `api/` check keeps the document clients are generated from honest: a route added without regenerating it fails the build, not the mobile team's next release.

<!-- include examples/apps/shelfie/.github/workflows/ci.yml#migrate-job -->

The staging database is a restore of the production backup, not production. A migration that takes a lock for four minutes on a table with three rows and forty minutes on the real one is found here.

<!-- include examples/apps/shelfie/.github/workflows/ci.yml#image-job -->

## 5. Health checks

| Endpoint | Answers | Give it to |
|---|---|---|
| `GET /livez` | The process is up | The liveness probe: restart on failure |
| `GET /readyz` | The process is up **and** PostgreSQL answers; fails as soon as shutdown starts | The load balancer and the readiness probe |
| `GET /version` | The build version, Go version and commit | Nothing automatic; block it at the balancer if you'd rather not publish it |

Both health endpoints are public, and `/readyz` shares one check between concurrent requests and reuses it for a second, so probing every second costs almost nothing.

Give each instance a **stop grace period of at least 30 seconds**. On SIGTERM Shelfie marks itself not ready, waits five seconds so the load balancer stops routing to it, lets in-flight requests finish, lets running jobs finish within their timeout, records a clean stop in `release_instances`, then closes the pool. A shorter grace period cuts a reader's request in half and leaves a job to be retried.

```yaml
# The shape of it, whatever runs your containers.
livenessProbe:  { httpGet: { path: /livez,  port: 8080 }, periodSeconds: 10 }
readinessProbe: { httpGet: { path: /readyz, port: 8080 }, periodSeconds: 5 }
terminationGracePeriodSeconds: 40
```

Run at least two instances, so a rolling deploy never takes the API away. Database connections are `instances × APP_DB_MAX_CONNS` (10 by default) plus the migration job and River's listener: keep the total below PostgreSQL's `max_connections`.

## 6. The first hour, and after

1. **Before the first reader.** Create an administrator (`grant-role`), turn on their authenticator app, and check `/ops/system` — the migration state, the pool and the readiness checks in one response. `go run ./cmd/api seed` refuses to run in production, so there is no default password to forget about.
2. **Check what sign-in thinks it has.** `GET /ops/auth/providers` lists which methods are actually configured. A missing Google secret looks exactly like a working app until someone tries it ([go-live checklist](../../sign-in/go-live.md)).
3. **Tell Pagebound the URL**, and watch the first delivery. `/ops/auth/rate-limits` shows `partner_webhooks`, and the audit log shows `partners.purchase.recorded`.
4. **Alert on** `/readyz` failures, the 5xx rate, the log lines `unhandled error`, `panic recovered` and `incident opened: error rate above threshold`, discarded or repeatedly failing jobs — `gorbital.mail.send` above all, because a reader who never gets a verification email doesn't complain, they leave — and database connection saturation.
5. **Back up the database and the encryption keys separately.** A backup you have never restored is a hope, not a backup.

## Where to go next

- [Methods](../../methods/index.md): every function, type and option the library gives you, with a runnable example each.
- [Recipes](../index.md#recipes): a smaller app for one problem — [receiving payment webhooks](../recipes/receiving-payment-webhooks.md), [a mobile backend on an external identity provider](../recipes/mobile-backend-with-an-idp.md), [multi-tenant invoicing](../recipes/multi-tenant-invoicing.md).
- [Running in production](../../guides/production.md), [observability](../../guides/observability.md) and [the ops API](../../guides/ops-api.md) for what comes after the first deploy.
