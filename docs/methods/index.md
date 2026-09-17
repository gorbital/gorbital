# Methods

<!-- Generated from the library's doc comments and Example functions by `go run -C internal/tools/refdocs . -methods -write`. Don't edit: change the Go source, or add text in internal/tools/refdocs/overlay/methods/<slug>.md. -->

Every exported constant, variable, function, type and method of the gorbital library, one page per package: its signature, its doc comment, runnable examples from the package's `Example` functions, and the release it arrived in. The pages are generated from the Go source, so they match `go doc` for the same version.

*Since* names the first release whose API listing (`api/*.txt`) has the identifier; `v0.2.0 (unreleased)` marks API added on this branch. What the tiers promise: [Stability](../guides/stability.md).

## Core

The root module, `gorbital.dev`: small packages every app uses, with no dependencies beyond the standard library, OpenTelemetry and `golang.org/x`.

| Package | Summary |
|---|---|
| [`gorbital.dev/actor`](actor.md) | Package actor records who is performing an operation, in a context.Context. |
| [`gorbital.dev/app`](app.md) | Package app runs an application's long-running work and shuts it down in a defined order. |
| [`gorbital.dev/audit`](audit.md) | Package audit defines audit events and the Recorder contract that any module uses to record who did what to which resource, and whether it worked. |
| [`gorbital.dev/buildinfo`](buildinfo.md) | Package buildinfo reports the version, commit and build time of the running binary, from Go build information or a version set at link time. |
| [`gorbital.dev/config`](config.md) | Package config provides configuration helpers for the composition root of an gorbital app: a Secret type that never leaks into logs or output, and environment lookup with *_FILE support for mounted secrets. |
| [`gorbital.dev/gorbital`](gorbital.md) | Package gorbital composes gorbital's modules into an application (ADR-0081). |
| [`gorbital.dev/gorbital/authhttp`](gorbital-authhttp.md) | Package authhttp is sign-in for apps on gorbital.Main: registration with email verification, passwords, sessions in a cookie or a bearer token, two-factor authentication with authenticator apps and passkeys, Google, Apple and GitHub sign-in, API keys and service accounts, platform roles, and the operators' account APIs under /ops/auth/users (ADR-0024, ADR-0038, ADR-0043 to ADR-0046, ADR-0058, ADR-0059, ADR-0070). |
| [`gorbital.dev/gorbital/flagshttp`](gorbital-flagshttp.md) | Package flagshttp is the client feature flags API as a gorbital module: GET /v1/flags tells a signed-in caller whether each flag declared with flags.Client() is on for them (ADR-0057). |
| [`gorbital.dev/gorbital/gorbitaltest`](gorbital-gorbitaltest.md) | Package gorbitaltest tests a gorbital app through its real middleware stack, on its own PostgreSQL database per test (ADR-0028): |
| [`gorbital.dev/gorbital/guard`](gorbital-guard.md) | Package guard provides route options that decide whether a request may reach a route's handler (ADR-0082). |
| [`gorbital.dev/gorbital/mailevents`](gorbital-mailevents.md) | Package mailevents is the mail events module: POST /v1/webhooks/resend receives Resend's signed bounce and complaint webhooks and puts the addresses that bounced permanently or complained on the suppression list, which the mail worker checks before every send (ADR-0062). |
| [`gorbital.dev/gorbital/opshttp`](gorbital-opshttp.md) | Package opshttp is the operations API as a gorbital module: the admin endpoints under /ops/ for runtime settings, feature flags, background jobs, the audit log, releases, email and its suppression list, file storage, sign-in methods and rate limits, system health, retention, live observability and incidents (ADR-0026, ADR-0051, ADR-0064). |
| [`gorbital.dev/gorbital/orgshttp`](gorbital-orgshttp.md) | Package orgshttp is organisations as a gorbital module, for multi-tenant apps (ADR-0023, ADR-0048): organisations with members holding one role each (owner, admin, member, and the roles the app's modules grant), invitations by email, a personal workspace per account, deletion with a restore period and the orgs_purge job, each organisation's own runtime settings (ADR-0056) and client feature flags (ADR-0057), and organisations' service accounts with their API keys (ADR-0058). |
| [`gorbital.dev/health`](health.md) | Package health serves liveness and readiness endpoints. |
| [`gorbital.dev/httpx`](httpx.md) | Package httpx provides the HTTP foundation of a gorbital app: a server that runs under app.Run with safe timeouts, security middleware, and the RFC 9457 problem+json error contract with an application-owned error mapping (ADR-0018). |
| [`gorbital.dev/httpx/ipfilter`](httpx-ipfilter.md) | Package ipfilter refuses requests by client address with 403 ip_not_allowed (ADR-0085): |
| [`gorbital.dev/httpx/timeout`](httpx-timeout.md) | Package timeout gives each request a deadline and answers 503 request_timeout when the handler hasn't started its response by then (ADR-0085): |
| [`gorbital.dev/mail`](mail.md) | Package mail defines email messages and the Sender contract implemented by provider modules such as mail/resend and mail/smtp (ADR-0025). |
| [`gorbital.dev/page`](page.md) | Package page provides cursor pagination and sorting for list endpoints: bounded limits, opaque cursors and allowlisted sort fields. |
| [`gorbital.dev/ratelimit`](ratelimit.md) | Package ratelimit provides token-bucket rate limiting keyed by a string (an IP address, account or API key): the Taker interface, an in-memory Limiter, and HTTP middleware. |
| [`gorbital.dev/requestid`](requestid.md) | Package requestid generates, validates and carries request IDs in a context.Context, so HTTP middleware, logging, audit and jobs share one correlation value (ADR-0030). |
| [`gorbital.dev/webhook`](webhook.md) | Package webhook verifies signed webhook requests: an HMAC-SHA256 signature over the raw body, and optionally a delivery ID and a timestamp checked against a replay window (ADR-0085). |

## Modules

One Go module per directory under `modules/`, each added to an app on its own.

| Package | Summary |
|---|---|
| [`gorbital.dev/modules/auditpg`](modules-auditpg.md) | Package auditpg stores audit events in PostgreSQL and queries them for operator APIs (ADR-0036). |
| [`gorbital.dev/modules/auth`](modules-auth.md) | Package auth provides the building blocks of email and password authentication: argon2id password hashing, session tokens and one-time codes stored only as hashes, email normalisation, session cookies, the request middleware, a permission catalog and plain authentication emails (ADR-0024, ADR-0038). |
| [`gorbital.dev/modules/auth/passkey`](modules-auth-passkey.md) | Package passkey provides passkeys (WebAuthn) for apps: registration and sign-in ceremonies checked against the relying party's ID and allowed origins, credential records to store, and the association files native apps need (ADR-0044). |
| [`gorbital.dev/modules/auth/passkey/passkeytest`](modules-auth-passkey-passkeytest.md) | Package passkeytest is a software passkey authenticator for tests. |
| [`gorbital.dev/modules/auth/social`](modules-auth-social.md) | Package social signs people in with Google, Apple (ADR-0046) and GitHub (ADR-0059): the web authorization code flow with state, nonce and PKCE, ID tokens from native apps checked against the app's client IDs, Apple's client secret, token revocation and server-to-server notifications, and GitHub's user and email API for a provider without OpenID Connect. |
| [`gorbital.dev/modules/auth/social/socialtest`](modules-auth-social-socialtest.md) | Package socialtest runs an in-process OpenID Connect provider standing in for Google or Apple in tests: it serves signing keys, a token endpoint and a revocation endpoint, and issues signed ID tokens, authorization codes and Apple notifications. |
| [`gorbital.dev/modules/devconsole`](modules-devconsole.md) | Package devconsole serves development-only JSON APIs under /_dev/ for a local console (ADR-0065): what the app wired, its routes, the environment variables it read (secrets only as set or unset), recent requests and log records with live streams, captured email, migration state and recent job runs. |
| [`gorbital.dev/modules/flags`](modules-flags.md) | Package flags provides feature flags: on/off switches declared in Go, targeted at organisations and users, rolled out to a stable percentage of them, stored in PostgreSQL only when changed, and applied on every instance without a restart (ADR-0057). |
| [`gorbital.dev/modules/idempotency`](modules-idempotency.md) | Package idempotency makes retried POST and PATCH requests safe (ADR-0060). |
| [`gorbital.dev/modules/jobs`](modules-jobs.md) | Package jobs runs background jobs on PostgreSQL with River (ADR-0033). |
| [`gorbital.dev/modules/jwt`](modules-jwt.md) | Package jwt authenticates requests carrying JSON Web Tokens from an external identity provider, such as Auth0, Clerk, Supabase, Firebase or Amazon Cognito (ADR-0085). |
| [`gorbital.dev/modules/mail/resend`](modules-mail-resend.md) | Package resend sends email with Resend (https://resend.com) through its HTTP API (ADR-0025, ADR-0037). |
| [`gorbital.dev/modules/mail/smtp`](modules-mail-smtp.md) | Package smtp sends email through any SMTP server: Amazon SES, Postmark, Mailgun, Google Workspace, your own server, or Mailpit in development (ADR-0025, ADR-0037). |
| [`gorbital.dev/modules/mail/suppressionpg`](modules-mail-suppressionpg.md) | Package suppressionpg stores the email suppression list in PostgreSQL (ADR-0062): addresses that bounced permanently or marked an email as spam, which must not receive email again. |
| [`gorbital.dev/modules/observability`](modules-observability.md) | Package observability shows how an application is doing across all its instances and keeps a record of incidents (ADR-0064). |
| [`gorbital.dev/modules/openapi`](modules-openapi.md) | Package openapi integrates Huma with gorbital (ADR-0027): an API on the standard http.ServeMux, problem+json errors produced by the application's error mapper, an API reference at /docs in the gorbital design (ADR-0049, rendered by package reference), and OpenAPI export. |
| [`gorbital.dev/modules/openapi/reference`](modules-openapi-reference.md) | Package reference renders an OpenAPI 3.1 document as an API reference in the gorbital design (ADR-0049): an overview, and a page per operation with its parameters, responses, request examples in curl, Go and TypeScript, response examples and "Try it". |
| [`gorbital.dev/modules/orgs`](modules-orgs.md) | Package orgs holds the building blocks for organisations in multi-tenant apps (ADR-0023, ADR-0048): organisation IDs, the membership check every organisation operation starts with, and invitation emails. |
| [`gorbital.dev/modules/postgres`](modules-postgres.md) | Package postgres connects gorbital apps to PostgreSQL: a pgx connection pool with OpenTelemetry tracing, transactions, error classification for repositories, a readiness check, goose migrations (ADR-0005, ADR-0032) and the organisation row-level security policies read (ADR-0061). |
| [`gorbital.dev/modules/postgres/pgtest`](modules-postgres-pgtest.md) | Package pgtest gives each test its own PostgreSQL database on the Docker PostgreSQL server started with `docker compose up -d --wait` (ADR-0028). |
| [`gorbital.dev/modules/ratelimitpg`](modules-ratelimitpg.md) | Package ratelimitpg shares rate limits across instances in PostgreSQL (ADR-0052). |
| [`gorbital.dev/modules/releases`](modules-releases.md) | Package releases records which build every instance of an app runs and answers which releases are running (ADR-0040). |
| [`gorbital.dev/modules/settings`](modules-settings.md) | Package settings provides runtime settings: non-secret tunables declared in Go with a default and bounds, stored in PostgreSQL only when changed, and applied on every instance without a restart (ADR-0031). |
| [`gorbital.dev/modules/storage`](modules-storage.md) | Package storage stores files for an app (ADR-0075): a Store is a bucket of objects addressed by key, with drivers for the local disk (development) and S3-compatible services (Amazon S3, DigitalOcean Spaces, Cloudflare R2, MinIO). |
| [`gorbital.dev/modules/storage/local`](modules-storage-local.md) | Package local stores objects on the local disk: development's storage driver (ADR-0075). |
| [`gorbital.dev/modules/storage/logarchive`](modules-storage-logarchive.md) | Package logarchive keeps an app's log records in its file storage (ADR-0079). |
| [`gorbital.dev/modules/storage/s3`](modules-storage-s3.md) | Package s3 stores objects in an S3-compatible service: Amazon S3, DigitalOcean Spaces, Cloudflare R2 or MinIO (ADR-0075), through the MinIO client. |
| [`gorbital.dev/modules/telemetry`](modules-telemetry.md) | Package telemetry sets up OpenTelemetry tracing and metrics and structured logs correlated with requests and traces (ADR-0007). |
