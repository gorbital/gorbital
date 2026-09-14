# APIStock v1 Scope

This page is the guard against scope creep. Any work not listed under **In v1** needs an accepted ADR before it starts.

## The v1 promise

A Go developer can run `aps new`, add PostgreSQL and authentication, generate a resource, and deploy a production-ready API. They own all generated code, and can upgrade to the next APIStock release without losing their edits.

## In v1

### Runtime kit (`apistock.dev/...`)

- Application lifecycle: ordered start/stop, graceful shutdown
- Configuration: typed structs, environment variables, `*_FILE` secrets, fail-fast validation
- HTTP: `net/http` ServeMux, middleware chain, error handler adapter, problem+json errors
- Health: `/livez` and `/readyz` with registered checks
- Observability: `log/slog`, OpenTelemetry traces and metrics, request ID and trace correlation
- Actor context and audit event contract
- Test kit: HTTP helpers, per-test PostgreSQL database

### Official modules

- **postgres**: pgx pool, transactions, goose migrations, sqlc conventions
- **auth**: email + password (argon2id), server-side sessions, email verification, password reset, login throttling, roles and permissions, security audit events
- **email**: sender interface, SMTP provider, one API provider
- **jobs**: River integration with transactional enqueue
- **auditpg**: append-only PostgreSQL audit store
- **ratelimit**: in-memory rate limiting middleware

### `aps` CLI

- `aps new`, `aps add`, `aps remove`
- `aps gen resource`, `aps gen migration`, `aps gen sql`
- `aps upgrade`
- `aps dev` with the local dev console
- `aps doctor`
- `--github` via the developer's own `gh` login, plus a generated CI workflow

## Later (post-v1, needs demand)

- OAuth/OIDC social login (v1.1), TOTP MFA (v1.2), passkeys (v2)
- Organisations / multi-tenancy module
- Admin screens module
- OpenAPI generation
- S3-compatible storage and outgoing webhooks modules
- Community module index and module author tooling (`aps module ...`)

## Not in scope: we will not build

- A custom HTTP router, handler context, ORM, logger or DI container
- Support for databases other than PostgreSQL
- A database abstraction layer or custom schema language
- A package registry or upload server (the Go module proxy is used instead)
- Plugins or dynamic code loading
- Deployment tooling, Kubernetes operators or infrastructure generation
- A frontend framework or UI kit
- Microservices tooling, a service mesh or a generic event bus
- A hosted dashboard or control plane (separate product, only with proven demand)
- A mobile app
- Dashboard-managed production configuration
- A hosted GitHub App, or storing GitHub tokens

## v1 is done when

1. `aps new` + `aps add ...` reproduces the hand-written reference app (golden test).
2. Running any `aps` command twice produces no second change.
3. An app generated with the previous release and edited by hand upgrades with `aps upgrade` in CI without losing edits.
4. The auth module has passed an external security review with all findings resolved.
5. Core, CLI and official modules are tagged `v1.0.0`.
