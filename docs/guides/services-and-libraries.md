# Services and libraries

Everything gorbital depends on, grouped by role: what it is, why it was chosen, where it's used, what problem it solves, what would happen without it, and how it's set up. Versions are the ones pinned in the repository's `go.mod` files and compose files when this page was written; the files are the source of truth.

The dependency rules behind this list ([ADR-0019](../adr/0019-module-dependency-rules.md)): the core library (`gorbital.dev`) may only depend on the standard library, the OpenTelemetry API and `golang.org/x`; everything heavier lives in a module that an app imports only if it uses it. `internal/archtest` fails the build when core gains another dependency.

## Runtime services

What a Full app talks to while it runs.

### PostgreSQL

| | |
|---|---|
| **What** | The relational database |
| **Why** | The only required service ([ADR-0005](../adr/0005-database-strategy.md)). It stores application data, users and sessions, runtime settings, feature flags, the job queue (River), the audit log and release records, so a production app needs nothing else: no Redis, no message broker |
| **Where** | `modules/postgres` (pool, transactions, migrations), every repository, `modules/settings`, `modules/flags` (feature flags), `modules/jobs`, `modules/auditpg`, `modules/releases`, `modules/ratelimitpg` (rate limits shared by every instance, in an unlogged table), `modules/idempotency` (responses to retried POST and PATCH requests), `modules/mail/suppressionpg` (the email suppression list), `modules/observability` (request counts per minute shared by every instance, incidents) |
| **Version** | `postgres:18` in `compose.yaml`; CI tests against the same image |
| **Without it** | A Full app doesn't start: `DATABASE_URL is required` |
| **Setup** | Development: `orb dev` or `docker compose up -d --wait`. Production: any managed PostgreSQL; set `DATABASE_URL` and run `cmd/migrate` before each release. Always in Docker locally, never installed on the machine ([ADR-0028](../adr/0028-local-development-environment.md)) |

### Email provider: Resend or SMTP

| | |
|---|---|
| **What** | Delivers email: [Resend](https://resend.com) through its HTTP API (`modules/mail/resend`), or any SMTP server (`modules/mail/smtp`, standard library `net/smtp`) |
| **Why** | Sign-up codes, password resets and security alerts must reach real inboxes with SPF and DKIM ([ADR-0025](../adr/0025-email-providers.md), [ADR-0037](../adr/0037-email-setup-and-delivery.md)) |
| **Where** | `internal/app/infra_mail.go` builds the sender; the `gorbital.mail.send` job worker calls it |
| **Without it** | Production refuses to start without credentials; users can't verify their email or reset passwords |
| **Setup** | `orb add mail`; [email guide](email.md) |

### Google and Apple

External identity providers for sign-in, used only when configured. Endpoints: Google `accounts.google.com` (authorization), `oauth2.googleapis.com` (token) and Google's JWKS; Apple `appleid.apple.com` (`/auth/authorize`, `/auth/token`, `/auth/keys`, `/auth/revoke`). See [authentication](authentication.md#google-and-apple-sign-in).

### An OTLP backend (optional)

Any OpenTelemetry-compatible collector or vendor receives traces and metrics when `OTEL_EXPORTER_OTLP_ENDPOINT` is set. Without it, telemetry stays in-process and logs still carry trace IDs.

## Development services

Run by `orb dev` from the app's `compose.yaml`. None of them run in production.

### Mailpit

| | |
|---|---|
| **What** | A fake email server with a web inbox ([mailpit.axllent.org](https://mailpit.axllent.org)) |
| **Why** | Every flow that sends email (sign-up, reset, alerts) must be testable locally without a provider account and without emailing real people by mistake |
| **Where** | `compose.yaml` service `mailpit` (`axllent/mailpit:v1.27`), SMTP on `127.0.0.1:1025`, inbox on `http://127.0.0.1:8025`. The app sends to it when `MAIL_DELIVERY` is `mailpit` (the development default), through `MAILPIT_SMTP_ADDR` |
| **How it works** | It accepts any message over SMTP without authentication and stores it; the web UI and its HTTP API (`/api/v1/messages`, `/api/v1/search`) show them. Tests read codes from that API |
| **Without it** | Development email sends fail and retry; nobody can finish sign-up locally. The app still starts |
| **Production** | Refused: `MAIL_DELIVERY=mailpit is for development` |
| **Setup** | Nothing: `orb dev` starts it. Ports: `MAILPIT_SMTP_PORT`, `MAILPIT_WEB_PORT`. The repository's own `compose.yaml` runs another instance on 51025 and 58025 for library tests |

### Grafana LGTM

| | |
|---|---|
| **What** | `grafana/otel-lgtm:0.33.0`: Grafana with Loki, Tempo and Prometheus-compatible storage and an OpenTelemetry collector, in one container |
| **Why** | To see traces, metrics and logs locally with no setup |
| **Where** | `compose.yaml` service `grafana`, profile `observability`; `orb dev --observability` starts it and sets `OTEL_EXPORTER_OTLP_ENDPOINT` |
| **Without it** | Nothing breaks; telemetry isn't exported |
| **Setup** | `orb dev --observability`, then http://127.0.0.1:3000 |

### Docker and Docker Compose

Run the services above, with health checks (`pg_isready`, `mailpit readyz`) that `docker compose up --wait` waits for. The Dockerfile builds production images (`golang:1.26` builder, `gcr.io/distroless/static-debian12:nonroot` runtime with `/api` and `/migrate`).

## Core Go libraries

Imported by generated apps through the gorbital modules.

| Library | Version | Used in | What it does and why | Without it |
|---|---|---|---|---|
| Go standard library | 1.26 | Everywhere | `net/http` routing (method and wildcard patterns), `log/slog`, `crypto/*`, `http.CrossOriginProtection`, `net/smtp`. gorbital prefers it before any dependency | — |
| [Huma](https://huma.rocks) `github.com/danielgtaylor/huma/v2` | v2.39.1 | `modules/openapi`, each module's `delivery/` | Code-first API: Go request and response types become validation, OpenAPI 3.1 and problem+json errors ([ADR-0027](../adr/0027-api-contract-and-docs.md), [spike](../../spikes/openapi/README.md)). Confined to `delivery/` | Hand-written validation and a spec that drifts from the code |
| [pgx](https://github.com/jackc/pgx) `github.com/jackc/pgx/v5` | v5.11.0 | `modules/postgres`, repositories, settings, flags, jobs, auditpg, releases | PostgreSQL driver and pool, with native types, `LISTEN/NOTIFY` (live settings and flags) and tracing hooks. Hand-written SQL on it, no ORM ([ADR-0032](../adr/0032-repository-sql.md)) | No database access |
| [goose](https://github.com/pressly/goose) `github.com/pressly/goose/v3` | v3.28.0 | `modules/postgres` (`postgres.Migrate`) | Applies SQL migration files in order, under an advisory lock, recording versions | Manual schema changes |
| [River](https://riverqueue.com) `github.com/riverqueue/river` | v0.47.0 | `modules/jobs` | Transactional job queue on PostgreSQL: retries, scheduling, a leader for periodic jobs, and enqueueing inside the same transaction as the data ([ADR-0033](../adr/0033-background-jobs.md)) | Email sent inside requests, no retries, a separate broker |
| `github.com/robfig/cron/v3` | v3.0.1 | `modules/jobs` | Parses 5-field cron schedules of job definitions | No cron schedules |
| [OpenTelemetry Go](https://opentelemetry.io/docs/languages/go/) `go.opentelemetry.io/otel`, SDK, OTLP HTTP exporters, `otelhttp` | v1.46.0 / contrib v0.71.0 | Core (trace API only), `modules/telemetry`, `modules/postgres`, `modules/jobs` | Traces, metrics and correlated logs; vendor-neutral export ([ADR-0007](../adr/0007-observability.md)) | No tracing; logs without trace IDs |
| OpenTelemetry Prometheus exporter `go.opentelemetry.io/otel/exporters/prometheus`, runtime instrumentation `go.opentelemetry.io/contrib/instrumentation/runtime`, [Prometheus client](https://github.com/prometheus/client_golang) `github.com/prometheus/client_golang` | v0.68.0 / v0.71.0 / v1.24.1 | `modules/telemetry` | Serves the meter provider's metrics in the Prometheus format on `METRICS_ADDR`, from its own registry, and records Go runtime metrics ([ADR-0063](../adr/0063-prometheus-metrics.md)). Compiled into every app; the listener is off by default | No `/metrics` endpoint; metrics only over OTLP |
| `golang.org/x/time` | v0.16.0 | Core `ratelimit` | Token-bucket limiter behind the sign-in rate limit | No brute-force protection per IP |

## Authentication libraries

All in `modules/auth` and its subpackages.

| Library | Version | Used in | What it does and why |
|---|---|---|---|
| `golang.org/x/crypto` | v0.57.0 | `modules/auth` | argon2id password hashing |
| [go-webauthn](https://github.com/go-webauthn/webauthn) `github.com/go-webauthn/webauthn` | v0.18.1 | `modules/auth/passkey` | WebAuthn registration and assertion verification: attestation formats, origins, RP ID, sign counters ([ADR-0044](../adr/0044-passkeys.md)). Security-critical parsing isn't hand-written |
| `github.com/fxamacker/cbor/v2` | v2.9.3 | `modules/auth/passkey/passkeytest` | CBOR encoding for the software authenticator used in tests |
| `golang.org/x/oauth2` | v0.37.0 | `modules/auth/social` | OAuth 2.0 authorization code flow with PKCE |
| [go-oidc](https://github.com/coreos/go-oidc) `github.com/coreos/go-oidc/v3` | v3.21.0 | `modules/auth/social` | Verifies Google and Apple ID tokens: signature from the provider's JWKS, issuer, audience, expiry ([ADR-0046](../adr/0046-google-and-apple-sign-in.md)) |
| [go-jose](https://github.com/go-jose/go-jose) `github.com/go-jose/go-jose/v4` | v4.1.4 | `modules/auth/social` | Signs the ES256 Apple client secret; verifies Apple's server-to-server notifications |
| `rsc.io/qr` | v0.2.0 | `modules/auth` | Renders the TOTP `otpauth://` URI as a QR code image, so no frontend library is needed |

TOTP (RFC 6238) and the AES-GCM keyring are implemented on the standard library in `modules/auth`.

## CLI libraries

| Library | Version | Used in | Why |
|---|---|---|---|
| [huh](https://github.com/charmbracelet/huh) `github.com/charmbracelet/huh` | v1.0.0 | `cli` | Interactive prompts with validation, accessible mode ([ADR-0035](../adr/0035-interactive-cli.md)) |
| `golang.org/x/term` | v0.46.0 | `cli` | Detects terminals, reads hidden secrets |

## Website libraries

gorbital.dev and docs.gorbital.dev are built by the separate gorbital-web repository from this repository's `docs/` ([ADR-0049](../adr/0049-public-docs-and-website.md)). Not part of apps.

| Library | Version | Why |
|---|---|---|
| [goldmark](https://github.com/yuin/goldmark) | v1.8.6 | Markdown to HTML with GitHub-flavoured extensions and heading IDs |
| [chroma](https://github.com/alecthomas/chroma) | v2.27.0 | Syntax highlighting in code blocks |
| `gorbital.dev/modules/openapi/reference` | this repository | Renders the API reference, the same renderer apps serve at `/docs` |

## Development and CI tools

Not imported; run with `go run pkg@version` or in CI (`.github/workflows/`).

| Tool | Version | Why |
|---|---|---|
| golangci-lint | v2.13.2 | Lint every module with the repository's `.golangci.yml` |
| govulncheck | v1.8.0 | Known vulnerabilities in dependencies and the Go toolchain |
| gitleaks | CI action | Secrets committed by mistake |
| GoReleaser and cosign | `release-cli.yml` | Build, sign and attest `orb` releases on `cli/v*` tags |
| gofmt, go vet | Go toolchain | Formatting and suspicious code |

## Testing helpers

| Package | What |
|---|---|
| `gorbital.dev/modules/postgres/pgtest` | A fresh database per test, cloned from a migrated template, dropped afterwards; skips without `GORBITAL_TEST_DATABASE_URL` |
| `gorbital.dev/modules/auth/passkey/passkeytest` | A software WebAuthn authenticator, so passkey flows run in `go test` |
| `gorbital.dev/modules/auth/social/socialtest` | A fake Google and Apple OpenID provider with signed tokens and notifications |
| `net/http/httptest` | End-to-end HTTP tests against the app's handler |

## Deliberately not used

| Not used | Instead | Why |
|---|---|---|
| Redis, Kafka, RabbitMQ | PostgreSQL (River, `LISTEN/NOTIFY`) | One required service; jobs commit with data |
| An ORM, sqlc | Hand-written SQL, one file per operation | Readable queries, no generated layer ([ADR-0032](../adr/0032-repository-sql.md)) |
| A router framework | `net/http.ServeMux` | The standard library's patterns are enough |
| A DI container | Constructors in `internal/app` | Wiring you can read and step through ([ADR-0004](../adr/0004-dependency-injection.md)) |
| JWT sessions | Opaque tokens hashed in PostgreSQL | Instant revocation, nothing to sign or rotate ([ADR-0038](../adr/0038-authentication-v0-2.md)) |
| Testcontainers, embedded PostgreSQL | Docker Compose services | The same PostgreSQL everywhere ([ADR-0028](../adr/0028-local-development-environment.md)) |
| Scalar, Swagger UI | `modules/openapi/reference` | Same design as the docs site, strict CSP, embedded fonts ([ADR-0049](../adr/0049-public-docs-and-website.md)) |
