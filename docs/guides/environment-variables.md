# Environment variables

Every environment variable gorbital reads, verified against the code: what reads it, its default, how it's validated, whether it's a secret, and where the value comes from. For a beginner's walkthrough of the credentials, see [Every key and credential](../sign-in/all-keys.md).

Three groups of programs read the environment, and each has its own variables:

| Reader | Where | Variables |
|---|---|---|
| A generated app (`cmd/api`, `cmd/migrate`, `cmd/seed`) | `internal/app/config.go`, `social.go`, `passkeys.go`, `infra_mail.go` | [App](#app-server), [database](#database), [authentication](#authentication), [email](#email), [telemetry](#telemetry) |
| An app on `gorbital.Main` (v0.2) | `gorbital.LoadConfig` | The same, with [a few checks moved](#apps-on-gorbitalmain) |
| Docker Compose, from the app's `compose.yaml` | Interpolated by `docker compose`, which reads `.env` itself | [Compose ports](#compose-ports) |
| `orb`, and the gorbital repository's tests | `cli/`, `modules/postgres/pgtest`, root `compose.yaml` | [CLI](#cli), [tests](#tests) |

## How the app reads configuration

- **Only `internal/app` reads the environment** (`LoadConfig` in `config.go`). Libraries take values as constructor arguments; `architecture_test.go` enforces it.
- **The app reads the process environment, not `.env`.** `orb dev` parses `.env` and passes it to the processes it starts, with variables already set in your shell taking precedence. With plain `go run`, run `set -a; . ./.env; set +a` first. Docker Compose reads `.env` on its own, for the port variables.
- **Every error is reported at once.** `LoadConfig` collects all problems and fails with `invalid configuration:` followed by one line per variable, so one start shows everything to fix.
- **Secrets can come from files.** For variables marked **Secret** below, `NAME_FILE=/path` reads the value from the file (`config.Source.Secret`, trailing newline trimmed). Setting both `NAME` and `NAME_FILE` fails with `config: both variable and _FILE variant are set: NAME`. Secrets are held as `config.Secret`, whose `String` and `LogValue` print `[redacted]`.
- **Empty means off; half-filled means stop.** An optional feature with all its variables empty is off. Setting some of a feature's variables but not the rest fails at start with the missing names ([ADR-0045](../adr/0045-sign-in-provider-setup.md)).
- **`APP_ENV` is required.** Without it the app refuses to start, so a deployment that forgets it can't run with development's relaxed checks. `orb dev` sets `development` when neither your shell nor `.env` sets it. `api openapi` reads no variables: the OpenAPI document describes the code, not a deployment.
- **`APP_ENV=production` tightens rules.** Marked **Prod** below: required values, https-only URLs, Mailpit refused, docs off by default.
- **Environment holds secrets and infrastructure only.** Tunables such as code lifetimes and the email sender are runtime settings in PostgreSQL, changed through `/ops/settings` ([runtime settings](runtime-settings.md)). A value is never in both.

## Apps on gorbital.Main

A v0.2 app built with [`gorbital.Main`](main-go.md) has no `internal/app/config.go`: `gorbital.LoadConfig` reads every variable on this page that a Full app reads, with the same names, defaults, messages and production refusals, and reports every problem at once, one line per variable ([Methods](../methods/gorbital.md#LoadConfig)). A v0.1 deployment's environment works unchanged. The configuration errors end the program with exit code 2.

A few checks belong to the driver, provider or authenticator an app passes instead of to `LoadConfig`, so an app that doesn't use one doesn't compile it ([ADR-0083](../adr/0083-modules-stack-migrations-and-ejection.md#configuration-what-moved-out-of-loadconfig)):

| Variable | In a v0.1 app | In an app on `gorbital.Main` |
|---|---|---|
| `AUTH_ENCRYPTION_KEYS` | Required in production | Its format is checked by `LoadConfig` when set; required in production, checked by `authhttp` at start. An app without sign-in, or with external identity tokens, needs none |
| `APPLE_PRIVATE_KEY` (or `APPLE_PRIVATE_KEY_FILE`), `WEBAUTHN_APPLE_APP_IDS`, `WEBAUTHN_ANDROID_APPS` | Parsed at start | Read and required together by `LoadConfig` as before; the Apple key and the app lists are checked by `authhttp` at start |
| `WEBAUTHN_ORIGINS` | Each origin on `WEBAUTHN_RP_ID` | Checked by `authhttp` at start |
| `STORAGE_DRIVER` other than `local` | The app opens S3 | Checked as before; the app passes the store: `gorbital.WithStorageFunc(func(cfg gorbital.Config) (storage.Store, error) { … cfg.Storage … })`. Without it, the start fails naming the option |
| `RESEND_API_KEY` | Required with `MAIL_DELIVERY=provider` | The app passes its provider with `gorbital.WithMailer` or `WithMailerFunc` (reading `cfg.Mail.ResendAPIKey`), which production requires; the provider checks its key |
| `RESEND_WEBHOOK_SECRET` | Format checked at start | Read; its format is checked at start by `mailevents.Module()` when the app has it, as a configuration error (exit code 2) |
| `SMTP_*` | Read by `infra_mail.go` after `orb add mail --provider smtp` | Not read by gorbital: build the sender with `gorbital.dev/modules/mail/smtp` in `WithMailerFunc` |

`authhttp`'s checks run in `gorbital.New` and `gorbital.Main` before anything connects, with v0.1's messages, and end the program with exit code 2 like `LoadConfig`'s ([Methods](../methods/gorbital-authhttp.md#Authenticator.CheckConfig)). `openapi` reads no variables, as `api openapi` doesn't in v0.1 apps.

## App server

Read in `config.go` by both presets.

| Variable | Required | Default | Example | Validation | Description |
|---|---|---|---|---|---|
| `APP_ENV` | **Yes** | none | `production` | `development` or `production`; missing fails with `APP_ENV is required` | Production switches logs to JSON, sends HSTS (365 days), enables a 5 s drain delay on shutdown, and applies every **Prod** rule on this page. The Dockerfile sets `production` |
| `APP_ADDR` | No | `127.0.0.1:8080` | `0.0.0.0:8080` | `host:port` | Listen address. Loopback by default so a development API isn't exposed on the network; containers need `0.0.0.0` (the Dockerfile sets it) |
| `APP_LOG_LEVEL` | No | `info` | `debug` | `debug`, `info`, `warn`, `error` | Minimum `slog` level |
| `APP_LOG_FORMAT` | No | empty | `json` | `json`, `text`, empty | Log encoding; empty means JSON in production and text elsewhere. `orb dev` sets `json` for the Dev Portal's log store and prints text ([ADR-0072](../adr/0072-local-log-store.md)) |
| `LOG_ARCHIVE_DIR` | No | `.orb/logs` | `/var/lib/acme/logs` | path | Full presets only. Where the hourly log archive spools the current hour before storing it gzipped in file storage under `logs/`; created on demand. **Prod**: in the generated image the default is `/home/nonroot/.orb/logs` inside the container, which a graceful stop empties into the bucket; point it at a mounted volume to keep a crashed instance's hour for the next start. Nothing is written until the `logs.archive.enabled` runtime setting is on ([ADR-0079](../adr/0079-hourly-log-archive.md), [observability guide](observability.md#the-hourly-log-archive)) |
| `APP_DOCS_ENABLED` | No | `true` in development, `false` in production | `true` | `true` or `false` | Serves `/docs` and the OpenAPI document (`/openapi.json`, `/openapi.yaml`). Off, both answer 404; `api openapi` still exports the document. Set `true` in production for a public API reference |
| `APP_CORS_ORIGINS` | No | empty | `https://app.example.com,http://localhost:3000` | Comma-separated origins (scheme, host, optional port; no user, path, query or trailing slash). **Prod:** https only | Browser origins allowed by CORS, trusted by the cross-origin protection, and accepted as `return_to` for Google and Apple web sign-in. Empty disables CORS |
| `APP_TRUSTED_PROXIES` | No | empty | `10.0.0.0/8,192.0.2.10` | Comma-separated CIDR ranges or IP addresses; ranges covering every address (`0.0.0.0/0`, `::/0`) are refused | Load balancers and reverse proxies whose `X-Forwarded-For` names the client, for rate limits, logs and audit events ([ADR-0052](../adr/0052-shared-rate-limits.md)). Requests from other addresses keep their own address and their forwarding headers are ignored. Empty trusts no header: correct only when clients connect directly |
| `APP_TRUSTED_CALLERS` | No | empty | `10.1.0.0/16` | Same as `APP_TRUSTED_PROXIES` | Gateways and internal services whose `X-Request-ID` and W3C trace context (`traceparent`, `tracestate`, `baggage`) the app keeps. Other clients get a generated request ID and a new trace linked to theirs, so they can't hide from tracing, force sampling or reuse another request's IDs. Matched against the client address after `APP_TRUSTED_PROXIES`: list your load balancer only if it sets those headers itself and drops clients' values |
| `APP_MAX_BODY_BYTES` | No | `1048576` | `5242880` | Positive integer | Request body limit; larger bodies get 413 `request_too_large` |
| `APP_REQUEST_TIMEOUT` | No | `30s` | `10s` | Go duration shorter than `60s` (the server's write timeout), or `0` | Apps on `gorbital.Main` only: a handler that hasn't started its response by then gets 503 `request_timeout`, and its context is cancelled |

## Operations

Apps on `gorbital.Main` with the operations API (`opshttp.Module()`, [ops API](ops-api.md#adding-it-to-an-app)).

| Variable | Required | Default | Example | Validation | Description |
|---|---|---|---|---|---|
| `OPS_ALLOWED_IPS` | No | empty | `10.8.0.0/16,2001:db8:42::/48,203.0.113.7` | Comma-separated CIDR ranges or IP addresses (`ipfilter.ParsePrefixes` in `gorbital.dev/httpx/ipfilter`): a range is masked (`10.0.0.5/8` is `10.0.0.0/8`), IPv4 written as IPv6 becomes IPv4, and IPv4-mapped ranges shorter than `/96` are refused; every bad entry is reported at once | Client addresses allowed to call `/ops/`; others get 403 `ip_not_allowed` before the sign-in check. Matched against the address after `APP_TRUSTED_PROXIES`, so set that behind a load balancer. Empty allows every address ([security layers](security-layers.md#ip-filter), [ADR-0085](../adr/0085-security-layers.md)) |

## Database

Full preset only.

| Variable | Required | Default | Example | Secret | Validation | Description |
|---|---|---|---|---|---|---|
| `DATABASE_URL` | **Yes** | none | `postgres://acme-api:acme-api@127.0.0.1:5432/acme-api?sslmode=disable` | **Secret** | Required by `app.New`, `cmd/migrate` and `cmd/seed` (`DATABASE_URL is required`); parsed and pinged by `postgres.Open` | PostgreSQL connection URL (pgx format). Development value from `compose.yaml`; production from your database provider, with `sslmode=require` or `verify-full`. Errors never include the URL |
| `APP_DB_MAX_CONNS` | No | `10` | `20` | 1 to 1000 | Pool size per instance. Keep instances × this below the server's `max_connections` |
| `APP_JOB_WORKERS` | No | `10` | `25` | 1 to 10000 | Concurrent jobs per instance on the default queue |

## Authentication

### Two-factor authentication

| Variable | Required | Default | Example | Secret | Description |
|---|---|---|---|---|---|
| `AUTH_ENCRYPTION_KEYS` | **Prod**; also `cmd/seed` | empty | `k2:…,k1:…` | **Secret** | AES-256-GCM keys that encrypt TOTP secrets. Format: comma-separated `id:base64`, each key exactly 32 bytes after base64 decoding; ids unique. The first key encrypts, all decrypt. Empty in development turns authenticator apps off (503 `mfa_unavailable`) and `orb dev` fills it. Generate: `echo "k1:$(openssl rand -base64 32)"`. Rotate with `cmd/api rotate-auth-keys` ([secrets and keys](secrets-and-keys.md#auth-encryption-keys)) |

### Passkeys

Read in `passkeys.go` ([ADR-0044](../adr/0044-passkeys.md)). No secrets.

| Variable | Required | Default (development) | Example | Validation | Description |
|---|---|---|---|---|---|
| `WEBAUTHN_RP_ID` | No (empty in production turns passkeys off) | `localhost` | `example.com` | Required when any other `WEBAUTHN_` variable is set | Relying party ID: the registrable domain every passkey is bound to. Changing it invalidates existing passkeys |
| `WEBAUTHN_ORIGINS` | With `WEBAUTHN_RP_ID` | `http://localhost:8080,http://localhost:3000` | `https://example.com,https://app.example.com` | Each on the RP ID or a subdomain; **Prod**: https | Origins allowed in WebAuthn client data |
| `WEBAUTHN_APPLE_APP_IDS` | No | empty | `ABCDE12345.com.example.app` | `TEAMID.bundle.id`, comma-separated | Served in `/.well-known/apple-app-site-association` under `webcredentials` |
| `WEBAUTHN_ANDROID_APPS` | No | empty | `com.example.app=SHA256:AB:…:EF+SHA256:12:…:56` | `package=SHA256:FP[+SHA256:FP…]`, comma-separated | Served in `/.well-known/assetlinks.json`; also allows the apps' `android:apk-key-hash:` origins |

In development, when both `WEBAUTHN_RP_ID` and `WEBAUTHN_ORIGINS` are empty, the defaults above apply. In production, both empty means passkey endpoints answer 503 `passkeys_unavailable`.

### Google, Apple and GitHub

Read in `social.go` ([ADR-0046](../adr/0046-google-and-apple-sign-in.md), [ADR-0059](../adr/0059-github-sign-in.md)).

| Variable | Required | Default | Example | Secret | Description |
|---|---|---|---|---|---|
| `APP_PUBLIC_URL` | With Google, Apple web or GitHub sign-in, in production | `http://localhost:8080` in development | `https://api.example.com` | No | The API's public origin: scheme and host, no path. Callback URLs are `<APP_PUBLIC_URL>/v1/auth/{google,apple,github}/callback`. **Prod**: https |
| `AUTH_DEFAULT_RETURN_TO` | With Google, Apple web or GitHub sign-in, in production (and in development with `APP_DOCS_ENABLED=false`) | `<APP_PUBLIC_URL>/docs` in development | `https://app.example.com/signed-in` | No | Where a browser sign-in ends when it names no `return_to`, or fails before its `return_to` is known. Absolute URL on `APP_PUBLIC_URL` or an `APP_CORS_ORIGINS` origin, no user information or fragment. **Prod**: https; checked at start |
| `GOOGLE_CLIENT_ID` | Enables Google | empty | `123-abc.apps.googleusercontent.com` | No | Web application client ID. Also an accepted audience for native ID tokens (Android's `serverClientId`) |
| `GOOGLE_CLIENT_SECRET` | With `GOOGLE_CLIENT_ID` | empty | `GOCSPX-…` | **Secret** | Web client secret, for the authorization code exchange |
| `GOOGLE_IOS_CLIENT_ID` | No; needs `GOOGLE_CLIENT_ID` | empty | `123-ios.apps.googleusercontent.com` | No | Accepted audience for ID tokens from the iOS app |
| `GOOGLE_ANDROID_CLIENT_ID` | No; needs `GOOGLE_CLIENT_ID` | empty | `123-and.apps.googleusercontent.com` | No | Accepted audience for ID tokens from the Android app |
| `APPLE_TEAM_ID` | With any Apple variable | empty | `ABCDE12345` | No | `iss` of the generated client secret |
| `APPLE_KEY_ID` | With any Apple variable | empty | `XYZ987WVU6` | No | `kid` of the generated client secret |
| `APPLE_PRIVATE_KEY` (or `APPLE_PRIVATE_KEY_FILE`) | With any Apple variable | empty | `-----BEGIN PRIVATE KEY-----…` | **Secret** | The `.p8` Sign in with Apple key: PKCS #8 PEM, ECDSA P-256. Parsed at start |
| `APPLE_SERVICES_ID` | This or `APPLE_BUNDLE_IDS` | empty | `com.example.web` | No | Client ID for web sign-in |
| `APPLE_BUNDLE_IDS` | This or `APPLE_SERVICES_ID` | empty | `com.example.app,com.example.app.dev` | No | Client IDs for native sign-in |
| `GITHUB_CLIENT_ID` | Enables GitHub | empty | `Ov23liAbCdEf12345678` | No | The GitHub OAuth app's client ID; web sign-in only |
| `GITHUB_CLIENT_SECRET` (or `GITHUB_CLIENT_SECRET_FILE`) | With `GITHUB_CLIENT_ID` | empty | 40 hexadecimal characters | **Secret** | The OAuth app's client secret, for the code exchange |

`.env.example` lists `APPLE_PRIVATE_KEY_FILE` and mentions `APPLE_PRIVATE_KEY` in its comment: a file is the recommended form, because multi-line values don't survive most `.env` parsers and environment dashboards.

## Email

Read in `config.go` and `infra_mail.go`. `infra_mail.go` is replaced by `orb add mail`, so exactly one provider's variables apply.

| Variable | Required | Default | Example | Secret | Description |
|---|---|---|---|---|---|
| `STORAGE_DRIVER` | No | `local` | `s3` | `local`, `s3`, `spaces`, `r2`, `minio` | File storage driver ([storage guide](storage.md)). **Prod**: `local` is refused |
| `STORAGE_LOCAL_DIR` | No | `.orb/storage` | `/var/lib/acme/files` | path | The local driver's directory |
| `STORAGE_ENDPOINT`, `STORAGE_REGION`, `STORAGE_BUCKET`, `STORAGE_ACCESS_KEY`, `STORAGE_SECRET_KEY` | For the S3 drivers | derived endpoint for `s3` and `spaces` | `s3.eu-west-1.amazonaws.com` | | The service and its credentials; `STORAGE_SECRET_KEY` is a secret |
| `STORAGE_PUBLIC_URL`, `STORAGE_PATH_STYLE`, `STORAGE_SIGNING_KEY` | No | none, `true` for minio, random | | | A public bucket's URL; bucket in the path; the key signing local links |
| `DEV_MAIL_SMTP_ADDR` | No | `127.0.0.1:1025` | `127.0.0.1:1035` | host:port | Where `orb dev`'s mail catcher listens and the app sends with `MAIL_DELIVERY=devmail` ([ADR-0074](../adr/0074-dev-mail-previews-and-env-editor.md)) |
| `MAIL_DELIVERY` | No | `devmail` in development, `provider` in production | `provider` | No | `devmail`, `mailpit` or `provider`. **Prod**: `devmail` and `mailpit` are refused. Provider credentials are only required when delivery is `provider` |
| `MAILPIT_SMTP_ADDR` | No | `127.0.0.1:1025` | `127.0.0.1:1035` | No | `host:port` of Mailpit's SMTP server, used when delivery is `mailpit` |
| `RESEND_API_KEY` | Resend, with delivery `provider` | empty | `re_…` | **Secret** | Resend API key, "Sending access" is enough |
| `RESEND_WEBHOOK_SECRET` | No | empty | `whsec_…` | **Secret** | Signing secret of the Resend webhook for bounces and complaints; empty turns `POST /v1/webhooks/resend` off (404). Must be `whsec_` and base64 (`RESEND_WEBHOOK_SECRET: resend: the webhook signing secret must be …`). [Email guide](email.md#connect-resends-webhook) |
| `SMTP_HOST` | SMTP, with delivery `provider` | empty | `smtp.postmarkapp.com` | No | SMTP server |
| `SMTP_PORT` | No | `587` | `465` | No | A port number (`SMTP_PORT must be a port number such as 587`) |
| `SMTP_TLS` | No | `starttls` | `tls` | No | `starttls`, `tls` (implicit TLS, usually 465) or `none` (local servers only; `SMTP_TLS must be starttls, tls or none`) |
| `SMTP_USERNAME` | No | empty | `apikey` | No | Enables SMTP AUTH |
| `SMTP_PASSWORD` | With `SMTP_USERNAME` | empty | | **Secret** | SMTP password or token |

The sender (`mail.from_name`, `mail.from_email`, `mail.reply_to`) is a runtime setting, not an environment variable ([email](email.md)).

## Telemetry

| Variable | Required | Default | Example | Description |
|---|---|---|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | No | empty | `http://127.0.0.1:4318` | Turns on OTLP/HTTP export of traces and metrics. Empty keeps telemetry in-process: logs still carry `trace_id` and `span_id`. `orb dev --observability` sets it to the local Grafana. The OpenTelemetry exporters read it, and the other standard `OTEL_EXPORTER_OTLP_*` variables (such as `OTEL_EXPORTER_OTLP_HEADERS` for an API key), directly |
| `METRICS_ADDR` | No | empty | `0.0.0.0:9464` | Turns on Prometheus metrics: a separate listener serving only `GET /metrics`, with no authentication. Validated as `host:port` with a numeric port that differs from `APP_ADDR`'s (`METRICS_ADDR … must use another port than APP_ADDR`). Bind it to a private interface or an unpublished container port, never the public one ([production](production.md#prometheus-metrics), [ADR-0063](../adr/0063-prometheus-metrics.md)). Empty: off. All presets |
| `DEV_CONSOLE_TOKEN` | No | empty | set by `orb dev` | Secret. Turns on the development-only [dev console APIs](dev-console.md) under `/_dev/` when `APP_ENV` is `development`; requests need it as `Authorization: Bearer`, a localhost `Host` and a loopback connection. 32 to 512 visible ASCII characters (`DEV_CONSOLE_TOKEN must be 32 to 512 visible ASCII characters`). Set in production, the app refuses to start (`DEV_CONSOLE_TOKEN is for local development only`). `orb dev` generates a new one per run and never writes it to a file: leave it empty in `.env` ([ADR-0065](../adr/0065-local-dev-console-apis.md)). All presets |
| `DEV_PORTAL_TOKEN` | No | random per run | set by `orb dev` | Secret, `orb dev`'s own (not the app's): the Dev Portal's session token, in the link it prints; the portal sets it as the `orb_portal` cookie ([Dev Portal](dev-portal.md)) |
| `DEV_PORTAL_PORT` | No | `3100` | `3101` | `orb dev`'s own: the port the Dev Portal listens on, at 127.0.0.1 (`orb dev --portal-port` overrides it) |

## Compose ports

Read by `docker compose` from `.env` (and by `orb dev`'s port check), never by the app. All bind to `127.0.0.1`.

| Variable | Default | Service | Also change |
|---|---|---|---|
| `POSTGRES_PORT` | `5432` | `postgres` | The port in `DATABASE_URL` |
| `MAILPIT_SMTP_PORT` | `1025` | `mailpit` SMTP (apps made before ADR-0074 that keep Mailpit) | The port in `MAILPIT_SMTP_ADDR` |
| `MAILPIT_WEB_PORT` | `8025` | `mailpit` web inbox | Full apps read it for the dev console's `/_dev/mail`, with `MAILPIT_SMTP_ADDR`'s host (validated as a port number) |
| `GRAFANA_PORT` | `3000` | `grafana` (profile `observability`) | Nothing |
| `MINIO_PORT`, `MINIO_CONSOLE_PORT` | `9000`, `9001` | `minio` (added by `orb add storage --driver minio`) | The host in `STORAGE_ENDPOINT` (`127.0.0.1:9000`) |
| `OTLP_HTTP_PORT` | `4318` | `grafana` OTLP receiver | Nothing; `orb dev --observability` points the app at it |

## CLI

Read by `orb`.

| Variable | Effect |
|---|---|
| `CI` | Any value: never prompt, as with `--no-input` |
| `ACCESSIBLE` | Any value: plain one-line prompts for screen readers, as with `--plain` |
| `NO_COLOR` | Any value: no colour in output |
| `GOSUMDB`, `GONOSUMDB`, `GOPRIVATE`, `GOINSECURE` | Read by `orb upgrade` through `go`: it refuses to fetch an earlier release from the module proxy when checksum verification is off for `gorbital.dev/cli` |

## Tests

Read by tests in the gorbital repository and in generated apps.

| Variable | Used by | Effect |
|---|---|---|
| `GORBITAL_TEST_DATABASE_URL` | `pgtest` (every database test) | PostgreSQL server URL. Tests create a database per test from a migrated template and drop it afterwards. Unset: database tests skip |
| `GORBITAL_REQUIRE_DB` | `pgtest` | `1`: a missing `GORBITAL_TEST_DATABASE_URL` fails instead of skipping. Set it whenever you claim tests pass |
| `GORBITAL_TEST_MAILPIT_SMTP` | `modules/mail/smtp`, example apps | Mailpit SMTP address, such as `127.0.0.1:51025` in the repository |
| `GORBITAL_TEST_MAILPIT_URL` | Same | Mailpit web URL, such as `http://127.0.0.1:58025`, to read delivered messages |
| `GORBITAL_REQUIRE_MAILPIT` | Same | `1`: missing Mailpit variables fail instead of skipping |
| `ORB_E2E` | `cli` tests | `1`: generate apps and run their test suites |
| `ORB_E2E_DOCKER` | `cli` tests | `1`: run `orb dev` in a new Full app against real Docker on free ports |
| `GORBITAL_POSTGRES_PORT`, `GORBITAL_MAILPIT_SMTP_PORT`, `GORBITAL_MAILPIT_WEB_PORT` | Root `compose.yaml` | Host ports of the repository's test services: `55432`, `51025`, `58025` |

## Minimal preset

A Minimal app reads only the [app server](#app-server) variables (`LOG_ARCHIVE_DIR` excepted: it has no file storage), `OTEL_EXPORTER_OTLP_ENDPOINT`, `METRICS_ADDR` and `DEV_CONSOLE_TOKEN`; its `.env.example` also has `GRAFANA_PORT` and `OTLP_HTTP_PORT` for `orb dev --observability`.

## Checked against `.env.example`

Both Full golden apps' `.env.example` files (`examples/full-single`, `examples/full-multi`, identical) list every variable the app reads, with these notes:

| Variable | Note |
|---|---|
| `APPLE_PRIVATE_KEY` | Read by the code; mentioned in the comment above `APPLE_PRIVATE_KEY_FILE` rather than as its own line, by design |
| `*_FILE` variants | Supported for every secret; listed only for `DATABASE_URL` and the Apple key |
| `SMTP_*` | Appear in the `orb:begin mail` block only after `orb add mail --provider smtp`; a new app lists `RESEND_API_KEY` there |
| `OPS_ALLOWED_IPS`, `APP_REQUEST_TIMEOUT` | Read by apps on `gorbital.Main` only, so not in the v0.1 golden apps' files; Shelfie's `.env.example` lists them |
